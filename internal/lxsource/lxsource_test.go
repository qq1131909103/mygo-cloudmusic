package lxsource

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// aSource is a Luoxue (LX) audio source in miniature: it declares itself,
// registers its handler, and reads a song the way the real ones do.
const aSource = `
'use strict'
const { EVENT_NAMES, request, on, send, env, version } = globalThis.lx

const httpFetch = (url, options) => new Promise((resolve, reject) => {
  request(url, options, (err, resp) => (err ? reject(err) : resolve(resp)))
})

const handleGetMusicUrl = async (source, musicInfo, quality) => {
  const resp = await httpFetch(BASE + "/url?songId=" + encodeURIComponent(musicInfo.songId) + "&quality=" + quality)
  const body = JSON.parse(resp.body)
  if (body.code !== 200) return Promise.reject(body.message)
  return body.url
}

on(EVENT_NAMES.request, ({ action, source, info }) => {
  if (action === "musicUrl") return handleGetMusicUrl(source, info.musicInfo, info.type)
  return Promise.reject("action not support: " + action)
})

send(EVENT_NAMES.inited, {
  status: true,
  sources: {
    wy: { actions: ["musicUrl"], qualitys: ["128k", "320k", "flac"] },
    kw: { actions: ["musicUrl"], qualitys: ["320k"] },
  },
})
`

var _ = aSource

// scriptWith keeps a script and answers its requests, the way the service
// a real one asks would.
func scriptWith(t *testing.T, answer func(url string) (int, string)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, said := answer(r.URL.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		fmt.Fprint(w, said)
	}))
	t.Cleanup(srv.Close)
	body := strings.ReplaceAll(aSource, "BASE", strconvQuote(srv.URL))
	path := filepath.Join(t.TempDir(), "source.js")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// strconvQuote writes a URL as the JavaScript string the script holds.
func strconvQuote(s string) string { return `"` + s + `"` }

// A source script declares the platforms it resolves, and is asked which
// song plays where: the id NetEase gave a song is what it goes by.
func TestResolvesASong(t *testing.T) {
	var asked string
	path := scriptWith(t, func(url string) (int, string) {
		asked = url
		return 200, `{"code": 200, "url": "https://cdn.test/h.mp3"}`
	})
	box, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := box.Platforms(); len(got) != 2 || got[0] != "wy" || got[1] != "kw" {
		t.Errorf("declares %v, want wy and kw", got)
	}
	if got := box.Qualitys("wy"); len(got) != 3 || got[2] != "flac" {
		t.Errorf("wy offers %v", got)
	}
	if got := box.Platform("wy"); got != "wy" {
		t.Errorf("asked for wy, got %q", got)
	}
	if got := box.Platform("mg"); got != "wy" {
		t.Errorf("asked for mg, got %q", got)
	}
	if said := box.Describe(); !strings.Contains(said, "2 个平台") {
		t.Errorf("it describes itself as %q", said)
	}

	url, err := box.URL("wy", Quality("exhigh"), map[string]any{"musicId": int64(347230), "name": "海阔天空"})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://cdn.test/h.mp3" {
		t.Errorf("answered %q", url)
	}
	if !strings.Contains(asked, "songId=347230") {
		t.Errorf("the script asked for %q, without the id", asked)
	}
	if !strings.Contains(asked, "quality=320k") {
		t.Errorf("the script asked for %q, without the quality", asked)
	}
}

// What a script says when it cannot resolve a song is kept, rather than
// answered as an empty address.
func TestRefusalIsKept(t *testing.T) {
	path := scriptWith(t, func(string) (int, string) {
		return 200, `{"code": 401, "message": "卡密已过期"}`
	})
	box, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.URL("wy", "320k", map[string]any{"musicId": int64(1)}); err == nil {
		t.Fatal("a refusal was answered as if it had resolved")
	} else if !strings.Contains(err.Error(), "卡密已过期") {
		t.Errorf("the refusal says %q", err)
	}
}

// An answer the script cannot read at all is an error too, not a panic.
func TestUnreadableAnswer(t *testing.T) {
	path := scriptWith(t, func(string) (int, string) { return 200, "not json" })
	box, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.URL("wy", "320k", map[string]any{"musicId": int64(1)}); err == nil {
		t.Error("an unreadable answer was taken for an address")
	}
}

// A file that runs but never declares itself is not an audio source, and
// says so rather than being kept as one that resolves nothing.
func TestNotASource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.js")
	if err := os.WriteFile(path, []byte("const answer = 42"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("a file that is not a source was loaded as one")
	} else if !strings.Contains(err.Error(), "不是洛雪") {
		t.Errorf("it says %q", err)
	}
}

// A script that cannot even be read says where it looked.
func TestMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "gone.js")); err == nil {
		t.Fatal("a missing file was loaded")
	} else if !strings.Contains(err.Error(), "读不到") {
		t.Errorf("it says %q", err)
	}
}

// A script is read and started once, and again when the file changes.
func TestLoadKeepsAndReloads(t *testing.T) {
	path := scriptWith(t, func(string) (int, string) {
		return 200, `{"code": 200, "url": "https://cdn.test/a.mp3"}`
	})
	first, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Error("an unchanged script was run again")
	}
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(aSource, "BASE", `"http://127.0.0.1:1"`)), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Error("a changed script was kept from before")
	}
}

// A quality is asked for the way scripts name them, from the level NetEase
// names it by.
func TestQuality(t *testing.T) {
	for _, c := range []struct{ level, want string }{
		{"standard", "128k"},
		{"higher", "192k"},
		{"exhigh", "320k"},
		{"lossless", "flac"},
		{"hires", "flac24bit"},
		{"something-else", "320k"},
	} {
		if got := Quality(c.level); got != c.want {
			t.Errorf("%q asked for as %q, want %q", c.level, got, c.want)
		}
	}
}
