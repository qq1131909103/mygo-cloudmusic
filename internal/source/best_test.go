package source

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"netease-music-mygo/internal/netease"
)

// bestScript is a Luoxue (LX) audio source in miniature, which declares
// the platforms and qualities it offers and answers with an address that
// says what it was asked for.
// fetchScript is a Luoxue (LX) audio source that asks its service for the
// quality it was asked for, the way the real ones do: the service decides
// what it answers, and may refuse a quality.
const fetchScript = `
const { EVENT_NAMES, request, on, send } = globalThis.lx
const fetch = (url) => new Promise((resolve, reject) => {
  request(url, {}, (err, resp) => (err ? reject(err) : resolve(resp)))
})
on(EVENT_NAMES.request, async ({ action, info }) => {
  if (action !== "musicUrl") return Promise.reject("action not support")
  const resp = await fetch(BASE + "/get?quality=" + info.type)
  const body = JSON.parse(resp.body)
  if (body.code !== 200) return Promise.reject(body.message)
  return body.url
})
send(EVENT_NAMES.inited, { status: true, sources: { wy: { actions: ["musicUrl"], qualitys: [QUALITYS] } } })
`
const bestScript = `
const { EVENT_NAMES, request, on, send } = globalThis.lx
on(EVENT_NAMES.request, ({ action, info }) => {
  if (action !== "musicUrl") return Promise.reject("action not support")
  return BASE + "/get?quality=" + info.type
})
send(EVENT_NAMES.inited, { status: true, sources: { wy: { actions: ["musicUrl"], qualitys: [QUALITYS] } } })
`

// scriptSaying keeps a script that offers the qualities given, and
// answers its requests with an address naming the quality asked for.
func scriptSaying(t *testing.T, qualitys ...string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"code": 200, "url": %q}`, srvURL(r)+"?quality="+r.URL.Query().Get("quality"))
	}))
	t.Cleanup(srv.Close)
	body := strings.ReplaceAll(fetchScript, "BASE", `"`+srv.URL+`"`)
	body = strings.ReplaceAll(body, "QUALITYS", `"`+strings.Join(qualitys, `", "`)+`"`)
	path := filepath.Join(t.TempDir(), "source.js")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// srvURL is the address the request came to, as the script would answer
// with.
func srvURL(r *http.Request) string { return "http://" + r.Host }

// scriptAt keeps a script that offers the qualities given and asks the
// service at base for them, the way a real one does: which qualities
// answer and which refuse is the service's to decide.
func scriptAt(t *testing.T, base string, qualitys ...string) string {
	body := strings.ReplaceAll(fetchScript, "BASE", `"`+base+`"`)
	body = strings.ReplaceAll(body, "QUALITYS", `"`+strings.Join(qualitys, `", "`)+`"`)
	path := filepath.Join(t.TempDir(), "source.js")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// wantSong is a song NetEase would not serve, which a source is asked
// about by its id.
func wantSong() netease.Track {
	tr := netease.Track{}
	tr.ID = 347230
	tr.Name = "海阔天空"
	tr.Artists = []netease.Artist{{Name: "Beyond"}}
	return tr
}

// The best of what a script says it has is what it is asked for when the
// best is wanted: the highest it offers, over the levels NetEase names.
// What a script is asked for, in order: the qualities it says it has,
// from the best down, and every level NetEase names after them, so that
// one which answers a quality it did not declare is still asked.
func TestQualityOrder(t *testing.T) {
	for _, c := range []struct {
		have []string
		want []string
		why  string
	}{
		{[]string{"128k", "320k", "flac", "flac24bit"}, []string{"flac24bit", "flac", "320k", "128k"}, "best first"},
		{[]string{"flac", "128k"}, []string{"flac", "128k"}, "as the script named them"},
		{[]string{"192k", "weird"}, []string{"weird", "192k"}, "a quality NetEase does not name is the best"},
		{nil, []string{"flac24bit", "flac", "320k", "192k", "128k"}, "nothing declared"},
	} {
		got := qualityOrder(c.have)
		if len(got) < len(c.want) {
			t.Errorf("%v (%s) gave %v, too short", c.have, c.why, got)
			continue
		}
		for i, want := range c.want {
			if got[i] != want {
				t.Errorf("%v (%s) gave %v, want it to start %v", c.have, c.why, got, c.want)
				break
			}
		}
	}
}

// A level is named by the quality a script writes, the way round Quality
// names them, and not at all for one NetEase does not name.
func TestLevelOfQuality(t *testing.T) {
	for _, c := range []struct{ quality, want string }{
		{"flac24bit", "hires"},
		{"flac", "lossless"},
		{"320k", "exhigh"},
		{"192k", "higher"},
		{"128k", "standard"},
		{"320", ""},
		{"", ""},
	} {
		if got := levelOfQuality(c.quality); got != c.want {
			t.Errorf("%q named %q, want %q", c.quality, got, c.want)
		}
	}
}

// Asking for the best asks a script for the highest quality it says it
// has, and says the level that quality is, so the app can show what plays.
func TestFallbackBestAsksTheScriptsBest(t *testing.T) {
	var asked string
	path := scriptSaying(t, "128k", "320k", "flac")
	found, err := Fallback([]Entry{{ID: "s", Name: "我的音源", Kind: KindScript, Value: path, Enabled: true}}, wantSong(), LevelBest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(found.URL, "quality=flac") {
		t.Errorf("the best answered %q, which does not name the quality asked for", found.URL)
	}
	if found.Level != "lossless" {
		t.Errorf("the best answered at %q, want lossless", found.Level)
	}
	if found.Source != "我的音源" {
		t.Errorf("the answer came from %q, want the source's own name", found.Source)
	}
	_ = asked
}

// A level the listener names is asked for as it is, and the answer says
// which source gave it.
func TestFallbackNamesTheSourceThatAnswered(t *testing.T) {
	path := scriptSaying(t, "128k", "320k")
	found, err := Fallback([]Entry{{ID: "s", Name: "我的音源", Kind: KindScript, Value: path, Enabled: true}}, wantSong(), "exhigh")
	if err != nil {
		t.Fatal(err)
	}
	if found.Source != "我的音源" {
		t.Errorf("answered by %q, want the source's own name", found.Source)
	}
	if found.Level != "exhigh" {
		t.Errorf("answered at %q, want exhigh", found.Level)
	}
	if !strings.Contains(found.URL, "quality=320k") {
		t.Errorf("answered %q, which does not name 320k", found.URL)
	}
}

// The sources are asked in the order the settings keep them, and the
// first that answers wins: what the listener put first is what plays.
func TestFallbackTakesTheFirstThatAnswers(t *testing.T) {
	first := scriptSaying(t, "320k")
	second := scriptSaying(t, "320k")
	found, err := Fallback([]Entry{
		{ID: "a", Name: "先", Kind: KindScript, Value: first, Enabled: true},
		{ID: "b", Name: "后", Kind: KindScript, Value: second, Enabled: true},
	}, wantSong(), "exhigh")
	if err != nil {
		t.Fatal(err)
	}
	if found.Source != "先" {
		t.Errorf("answered by %q, want the one kept first", found.Source)
	}
	if found.URL == "" {
		t.Error("no address answered")
	}
}

// A script's own scripts may answer one quality and refuse the next: what
// it is asked for goes from the best down, and the first that answers is
// what plays. A script that refuses its best still plays at the quality
// below it.
func TestFallbackStepsDownToWhatAnswers(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("quality")
		asked = append(asked, q)
		w.Header().Set("Content-Type", "application/json")
		// The best two are refused, as a service that has run out of
		// them does; 320k answers.
		if q == "flac24bit" || q == "flac" {
			fmt.Fprint(w, `{"code": 401, "message": "该音质暂不可用"}`)
			return
		}
		fmt.Fprintf(w, `{"code": 200, "url": "https://cdn.test/hi.mp3?q=%s"}`, q)
	}))
	defer srv.Close()

	path := scriptAt(t, srv.URL, "flac24bit", "flac", "320k")
	found, err := Fallback([]Entry{{ID: "s", Name: "HYW", Kind: KindScript, Value: path, Enabled: true}}, wantSong(), LevelBest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(found.URL, "q=320k") {
		t.Errorf("answered %q, want the 320k the script would give", found.URL)
	}
	if found.Level != "exhigh" {
		t.Errorf("answered at %q, want exhigh: what it stepped down to", found.Level)
	}
	want := []string{"flac24bit", "flac", "320k"}
	if len(asked) != len(want) {
		t.Fatalf("asked for %v, want %v", asked, want)
	}
	for i := range want {
		if asked[i] != want[i] {
			t.Errorf("asked for %v, want %v", asked, want)
			break
		}
	}
}

// A level the listener names is asked for alone: the app does not quietly
// hand them a worse file than the one they chose.
func TestFallbackHonorsTheChosenLevel(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Query().Get("quality"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code": 401, "message": "该音质暂不可用"}`)
	}))
	defer srv.Close()

	path := scriptAt(t, srv.URL, "flac24bit", "flac", "320k")
	if _, err := Fallback([]Entry{{ID: "s", Kind: KindScript, Value: path, Enabled: true}}, wantSong(), "lossless"); err == nil {
		t.Fatal("a refused quality was answered as if it had resolved")
	}
	if len(asked) != 1 || asked[0] != "flac" {
		t.Errorf("asked for %v, want only flac", asked)
	}
}
