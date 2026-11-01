// Package lxsource runs a Luoxue (LX Music) custom audio source: the
// JavaScript file the LX client loads to find where a song plays, run
// here instead of in that client, so that a listener who has one can use
// it without installing anything else.
//
// Such a script is not a library. It lists nothing: it is asked where one
// song plays - an event carrying the song's ids and names - and answers
// with an address. That suits exactly the song a listener found in
// NetEase and NetEase will not serve: NetEase names it, and the script
// finds where else it plays.
//
// Only what a script of that kind reads from globalThis.lx is provided:
// EVENT_NAMES, request, on, send, env and version, as the LX client
// provides them, plus the console and the timers it may reach for. A
// script that wants the rest of Node (Buffer, URL, TextEncoder) is not
// one this app can run, and says so rather than failing silently.
package lxsource

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// The events the LX client sends and a script answers to.
const (
	eventRequest     = "request"
	eventInited      = "inited"
	eventUpdateAlert = "updateAlert"
)

// lxVersion and lxEnv are what a script is told it is running in: the
// names it puts in a User-Agent, and the branch of its own code it takes.
// Both are the desktop client's, which is what it was written for.
const (
	lxVersion = "2.11.0"
	lxEnv     = "desktop"
)

// How long a script may take to say it is ready, and how long one of its
// own requests may take.
const (
	loadWithin    = 15 * time.Second
	resolveWithin = 30 * time.Second
	requestWithin = 20 * time.Second
)

// maxTurns bounds how many times a script may be run on to get to an
// answer, so that one that never answers is given up on rather than
// spinning forever.
const maxTurns = 20000

// ErrNotASource is a file that runs but is not an audio source: one that
// never declares itself.
var ErrNotASource = errors.New("这个文件不是洛雪（LX）音源脚本：它没有声明自己会解析哪些平台")

// errStuck is what a script gets when it stops without answering: it is
// waiting for something this app does not provide.
var errStuck = errors.New("音源脚本没有给出结果")

// Sandbox is a loaded audio source script, ready to be asked where a
// song plays.
type Sandbox struct {
	path string

	// mu guards everything below: a script is not reentrant, so one
	// question is answered at a time.
	mu       sync.Mutex
	vm       *goja.Runtime
	drain    *goja.Program
	handlers map[string][]goja.Callable
	timers   map[uint32]*timer
	nextID   uint32
	sources  []Setting
	ready    bool
	said     string
}

// Setting is one platform a script says it can resolve, and the
// qualities it offers there.
type Setting struct {
	Platform string
	Qualitys []string
}

// timer is a script's own setTimeout or setInterval, waiting its turn.
type timer struct {
	fn     goja.Callable
	due    time.Time
	every  time.Duration
	repeat bool
}

// cache keeps the scripts already run, so that one used for every song is
// read and started once: keyed by path, and dropped when the file it was
// read from changes.
var cache sync.Map // path -> *cached

type cached struct {
	box   *Sandbox
	size  int64
	mtime time.Time
}

// Load runs the script at path and returns it ready to be asked. A script
// already run, and unchanged since, is returned as it is.
func Load(path string) (*Sandbox, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, errors.New("读不到这个音源文件：" + err.Error())
	}
	if hit, ok := cache.Load(path); ok {
		c := hit.(*cached)
		if c.size == info.Size() && c.mtime.Equal(info.ModTime()) {
			return c.box, nil
		}
	}
	box, err := run(path)
	if err != nil {
		return nil, err
	}
	cache.Store(path, &cached{box: box, size: info.Size(), mtime: info.ModTime()})
	return box, nil
}

// run reads a script and runs it until it declares itself.
func run(path string) (*Sandbox, error) {
	code, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("读不到这个音源文件：" + err.Error())
	}
	s := &Sandbox{
		path:     path,
		vm:       goja.New(),
		drain:    goja.MustCompile("", "void 0", false),
		handlers: map[string][]goja.Callable{},
		timers:   map[uint32]*timer{},
	}
	s.provide()
	if _, err := s.vm.RunString(string(code)); err != nil {
		return nil, errors.New("音源脚本没能跑起来：" + err.Error())
	}
	if err := s.pump(func() bool { return s.ready }, loadWithin); err != nil {
		if s.said != "" {
			return nil, fmt.Errorf("%w（脚本说：%s）", ErrNotASource, s.said)
		}
		return nil, ErrNotASource
	}
	return s, nil
}

// Platforms are the platforms the script says it resolves, in the order
// it declared them.
func (s *Sandbox) Platforms() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.sources))
	for _, st := range s.sources {
		out = append(out, st.Platform)
	}
	return out
}

// Qualitys are the qualities the script offers for a platform, or nil if
// it does not resolve that one.
func (s *Sandbox) Qualitys(platform string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.sources {
		if st.Platform == platform {
			return append([]string(nil), st.Qualitys...)
		}
	}
	return nil
}

// URL asks the script where the song plays, at the platform and quality
// asked for, and returns the address it answers with. The info is what
// the script reads the song from: its id, its name, who sings it.
func (s *Sandbox) URL(platform, quality string, info map[string]any) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return "", ErrNotASource
	}
	var handler goja.Callable
	if list := s.handlers[eventRequest]; len(list) > 0 {
		handler = list[0]
	}
	if handler == nil {
		return "", errors.New("这个音源脚本没有注册请求处理器")
	}
	call := map[string]any{
		"action": "musicUrl",
		"source": platform,
		"info":   map[string]any{"musicInfo": withNames(info, platform), "type": quality},
	}
	got, err := handler(goja.Undefined(), s.vm.ToValue(call))
	if err != nil {
		return "", errors.New("音源脚本拒绝了这次请求：" + err.Error())
	}
	if url, ok := got.Export().(string); ok {
		return strings.TrimSpace(url), nil
	}
	promise, ok := got.Export().(*goja.Promise)
	if !ok {
		return "", errors.New("音源脚本没有返回地址")
	}
	if err := s.pump(func() bool { return promise.State() != goja.PromiseStatePending }, resolveWithin); err != nil {
		return "", err
	}
	switch promise.State() {
	case goja.PromiseStateFulfilled:
		said, _ := promise.Result().Export().(string)
		if strings.TrimSpace(said) == "" {
			return "", errors.New("音源脚本解析出了空地址（这首歌它那边也没有）")
		}
		return strings.TrimSpace(said), nil
	case goja.PromiseStateRejected:
		why := ""
		switch v := promise.Result().Export().(type) {
		case string:
			why = v
		case error:
			why = v.Error()
		}
		if why == "" {
			why = "没有说明原因"
		}
		return "", errors.New("音源脚本解析不了这首歌：" + why)
	}
	return "", errStuck
}

// withNames fills in the names a script expects to find, whichever of
// them the caller left out: it reads a song's id from any of several
// keys, and takes the platform, and the platform's own name for the song
// it is given, for granted.
func withNames(info map[string]any, platform string) map[string]any {
	out := make(map[string]any, len(info)+4)
	for k, v := range info {
		out[k] = v
	}
	out["platform"] = platform
	out["source"] = platform
	if id, ok := out["musicId"]; ok {
		if _, given := out["id"]; !given {
			out["id"] = id
		}
		if _, given := out["songId"]; !given {
			out["songId"] = id
		}
	}
	return out
}

// provide gives the script the five things it reads from globalThis.lx,
// and the console and timers it may reach for.
func (s *Sandbox) provide() {
	vm := s.vm
	lx := vm.NewObject()
	lx.Set("EVENT_NAMES", map[string]string{
		"request":     eventRequest,
		"inited":      eventInited,
		"updateAlert": eventUpdateAlert,
	})
	lx.Set("env", lxEnv)
	lx.Set("version", lxVersion)
	lx.Set("request", s.hostRequest)
	lx.Set("on", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		if fn, ok := goja.AssertFunction(call.Argument(1)); ok {
			s.handlers[name] = append(s.handlers[name], fn)
		}
		return goja.Undefined()
	})
	lx.Set("send", func(call goja.FunctionCall) goja.Value {
		switch call.Argument(0).String() {
		case eventInited:
			s.readInited(call.Argument(1))
			s.ready = true
		case eventUpdateAlert:
			// The script saying a newer version of itself exists is no
			// business of this app's: this is not the client updating
			// it, and the listener did not ask for that.
		}
		return goja.Undefined()
	})
	vm.Set("lx", lx)

	say := vm.NewObject()
	note := func(call goja.FunctionCall) goja.Value {
		parts := make([]string, 0, len(call.Arguments))
		for _, v := range call.Arguments {
			parts = append(parts, v.String())
		}
		s.said = strings.Join(parts, " ")
		return goja.Undefined()
	}
	for _, name := range []string{"log", "info", "warn", "error", "debug"} {
		say.Set(name, note)
	}
	vm.Set("console", say)

	vm.Set("setTimeout", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(s.arm(call, false))
	})
	vm.Set("setInterval", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(s.arm(call, true))
	})
	clear := func(call goja.FunctionCall) goja.Value {
		delete(s.timers, uint32(call.Argument(0).ToInteger()))
		return goja.Undefined()
	}
	vm.Set("clearTimeout", clear)
	vm.Set("clearInterval", clear)

	// Two things scripts reach for that the language does not have: the
	// browser's pair of base64 helpers.
	vm.Set("atob", func(s string) (string, error) {
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return "", err
		}
		return string(b), nil
	})
	vm.Set("btoa", func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) })
}

// readInited takes what a script says it can resolve.
func (s *Sandbox) readInited(v goja.Value) {
	obj, ok := v.(*goja.Object)
	if !ok {
		return
	}
	sources, ok := obj.Get("sources").(*goja.Object)
	if !ok {
		return
	}
	for _, key := range sources.Keys() {
		one, ok := sources.Get(key).(*goja.Object)
		if !ok {
			continue
		}
		set := Setting{Platform: key}
		if list, ok := one.Get("qualitys").(*goja.Object); ok {
			set.Qualitys = qualitiesOf(list)
		}
		s.sources = append(s.sources, set)
	}
}

// qualitiesOf reads a platform's qualities, written either as the list
// scripts usually write them in, or as the object a few write instead.
func qualitiesOf(list *goja.Object) []string {
	if list.ClassName() != "Array" {
		return list.Keys()
	}
	n := int(list.Get("length").ToInteger())
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, list.Get(strconv.Itoa(i)).String())
	}
	return out
}

// arm records a timer for the pump to fire.
func (s *Sandbox) arm(call goja.FunctionCall, repeat bool) uint32 {
	fn, ok := goja.AssertFunction(call.Argument(0))
	if !ok {
		return 0
	}
	ms := call.Argument(1).ToInteger()
	if ms < 0 {
		ms = 0
	}
	every := time.Duration(ms) * time.Millisecond
	s.nextID++
	s.timers[s.nextID] = &timer{fn: fn, due: time.Now().Add(every), every: every, repeat: repeat}
	return s.nextID
}

// pump runs what a script has left to run - the continuations of its
// promises, and its timers - until what is waited for has happened, or
// until it runs out of things to do, or until the time given is up.
func (s *Sandbox) pump(until func() bool, within time.Duration) error {
	deadline := time.Now().Add(within)
	for turn := 0; turn < maxTurns; turn++ {
		// Running anything at all is what lets goja finish what the last
		// run left pending: its promise continuations.
		if _, err := s.vm.RunProgram(s.drain); err != nil {
			return errors.New("音源脚本出错了：" + err.Error())
		}
		if until() {
			return nil
		}
		due, id := s.firstDue()
		if id == 0 {
			return errStuck
		}
		if due.After(deadline) {
			return errors.New("音源脚本等得太久了")
		}
		if wait := time.Until(due); wait > 0 {
			time.Sleep(wait)
		}
		s.fire(id)
	}
	return errors.New("音源脚本转得太久了")
}

// firstDue is the timer that will come first, and its id.
func (s *Sandbox) firstDue() (time.Time, uint32) {
	var when time.Time
	var id uint32
	for key, t := range s.timers {
		if id == 0 || t.due.Before(when) {
			when, id = t.due, key
		}
	}
	return when, id
}

// fire runs a timer, and arms it again if it repeats.
func (s *Sandbox) fire(id uint32) {
	t := s.timers[id]
	if t == nil {
		return
	}
	if t.repeat {
		t.due = time.Now().Add(t.every)
	} else {
		delete(s.timers, id)
	}
	if _, err := t.fn(goja.Undefined()); err != nil {
		// What a timer throws is the script's business: the answer it
		// was wanted for is given up on, if it never comes.
		s.said = err.Error()
	}
}

// hostRequest is the script's request(url, options, callback). It is
// answered on the spot: the LX client's is asynchronous, but a script
// only ever waits for the answer, so answering before the call returns
// saves the machinery a real event loop would need.
func (s *Sandbox) hostRequest(call goja.FunctionCall) goja.Value {
	raw := call.Argument(0).String()
	options := call.Argument(1)
	back := call.Argument(2)
	if _, ok := goja.AssertFunction(back); !ok {
		back, options = call.Argument(1), goja.Undefined()
	}
	cb, ok := goja.AssertFunction(back)
	if !ok {
		return goja.Undefined()
	}
	resp, err := s.fetch(raw, options)
	var args []goja.Value
	if err != nil {
		args = []goja.Value{s.vm.NewGoError(err), goja.Undefined(), goja.Undefined()}
	} else {
		// The answer is given as an object under the names the LX client
		// uses: a script reads resp.body, and one written either way
		// reads the body out of the third argument too.
		said := s.vm.NewObject()
		said.Set("statusCode", resp.StatusCode)
		said.Set("statusMessage", resp.Status)
		said.Set("headers", resp.Headers)
		said.Set("body", resp.Body)
		args = []goja.Value{goja.Null(), said, s.vm.ToValue(resp.Body)}
	}
	if _, cerr := cb(goja.Undefined(), args...); cerr != nil {
		panic(s.vm.NewGoError(cerr))
	}
	return goja.Undefined()
}

// answer is what a script finds in resp: the shape the LX client gives it.
type answer struct {
	StatusCode int
	Status     string
	Headers    map[string]string
	Body       string
}

// fetch does what the script asked for: one HTTP request, GET unless it
// said otherwise.
func (s *Sandbox) fetch(raw string, options goja.Value) (*answer, error) {
	method := http.MethodGet
	body := ""
	headers := map[string]string{}
	if obj, ok := options.(*goja.Object); ok {
		if m := obj.Get("method"); m != nil && !goja.IsUndefined(m) && !goja.IsNull(m) {
			method = strings.ToUpper(m.String())
		}
		if b := obj.Get("body"); b != nil && !goja.IsUndefined(b) && !goja.IsNull(b) {
			body = b.String()
		}
		if h, ok := obj.Get("headers").(*goja.Object); ok {
			for _, key := range h.Keys() {
				headers[key] = h.Get(key).String()
			}
		}
	}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, raw, reader)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "lx-music-"+lxEnv+"/"+lxVersion)
	}
	client := &http.Client{Timeout: requestWithin}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	flat := make(map[string]string, len(res.Header))
	for key := range res.Header {
		flat[key] = res.Header.Get(key)
	}
	return &answer{
		StatusCode: res.StatusCode,
		Status:     res.Status,
		Headers:    flat,
		Body:       string(b),
	}, nil
}

// Platform is the source to ask the script for, given the one wanted:
// what it declares if it declares it, else the first it declares, else
// the one wanted, so that a script is never asked for a platform it says
// nothing about while declaring others.
func (s *Sandbox) Platform(want string) string {
	have := s.Platforms()
	for _, p := range have {
		if p == want {
			return want
		}
	}
	if len(have) > 0 {
		return have[0]
	}
	return want
}

// Quality is the quality to ask a script for, named the way scripts name
// them, from the level NetEase names it by.
func Quality(level string) string {
	switch level {
	case "standard":
		return "128k"
	case "higher":
		return "192k"
	case "exhigh":
		return "320k"
	case "lossless":
		return "flac"
	case "hires":
		return "flac24bit"
	}
	return "320k"
}

// Describe says in one line what a script can do, for the settings to
// show beside the file it was read from.
func (s *Sandbox) Describe() string {
	if len(s.sources) == 0 {
		return "可以，这是一个洛雪音源脚本"
	}
	lines := make([]string, 0, len(s.sources))
	for _, st := range s.sources {
		line := st.Platform
		if len(st.Qualitys) > 0 {
			line += "（" + strings.Join(st.Qualitys, "/") + "）"
		}
		lines = append(lines, line)
	}
	return "可以，这是一个洛雪音源脚本，支持 " + strconv.Itoa(len(s.sources)) + " 个平台：" + strings.Join(lines, "、")
}
