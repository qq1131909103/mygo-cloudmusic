package source

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"netease-music-mygo/internal/netease"
)

// linkServer answers with body under the content type given.
func linkServer(t *testing.T, ctype, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ctype)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A link pasted without its scheme works as it is, and the tracks of a
// playlist are listed with it.
func TestLinkWithoutScheme(t *testing.T) {
	srv := linkServer(t, "audio/x-mpegurl", "#EXTM3U\n#EXTINF:239,Beyond - 海阔天空\nhttp://cdn/h.mp3\n")

	tracks, err := Link(strings.TrimPrefix(srv.URL, "http://") + "/list.m3u")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].PlayURL != "http://cdn/h.mp3" {
		t.Fatalf("listed %+v", tracks)
	}
}

// A link to an m3u playlist lists what it names, with the name and length
// each entry gives, and its relative addresses read from where it sits.
func TestLinkM3U(t *testing.T) {
	srv := linkServer(t, "audio/x-mpegurl; charset=utf-8", strings.Join([]string{
		"#EXTM3U",
		"#EXTINF:213,Artist - Song",
		"/song.mp3",
		"",
		"#EXTINF:-1,Live Set",
		"https://other.example/live.ogg",
	}, "\n"))

	tracks, err := Link(srv.URL + "/list.m3u")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 {
		t.Fatalf("listed %d tracks: %+v", len(tracks), tracks)
	}
	if tracks[0].Name != "Song" || artistOf(tracks[0]) != "Artist" {
		t.Errorf("first track is %q by %q", tracks[0].Name, artistOf(tracks[0]))
	}
	if tracks[0].Duration != 213000 {
		t.Errorf("first track is %d ms", tracks[0].Duration)
	}
	if want := srv.URL + "/song.mp3"; tracks[0].PlayURL != want {
		t.Errorf("first track plays from %q, want %q", tracks[0].PlayURL, want)
	}
	if tracks[1].Name != "Live Set" || tracks[1].PlayURL != "https://other.example/live.ogg" {
		t.Errorf("second track is %+v", tracks[1])
	}
}

// A link to a pls playlist lists the files it names.
func TestLinkPLS(t *testing.T) {
	srv := linkServer(t, "audio/x-scpls", strings.Join([]string{
		"[playlist]",
		"File1=/a.mp3",
		"Title1=Someone - Something",
		"Length1=42",
		"NumberOfEntries=1",
		"Version=2",
	}, "\n"))

	tracks, err := Link(srv.URL + "/list.pls")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("listed %d tracks: %+v", len(tracks), tracks)
	}
	if tracks[0].Name != "Something" || artistOf(tracks[0]) != "Someone" || tracks[0].Duration != 42000 {
		t.Errorf("listed %+v", tracks[0])
	}
	if want := srv.URL + "/a.mp3"; tracks[0].PlayURL != want {
		t.Errorf("plays from %q, want %q", tracks[0].PlayURL, want)
	}
}

// A link to what an HTTP source answers with lists its tracks, the same
// way the endpoint itself would.
func TestLinkJSON(t *testing.T) {
	srv := linkServer(t, "application/json", `{"tracks":[{"title":"晴天","artists":["周杰伦"],"dt":269000,"playUrl":"http://x/a.mp3","picUrl":"http://x/a.jpg"}]}`)

	tracks, err := Link(srv.URL + "/songs")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Name != "晴天" || tracks[0].PlayURL != "http://x/a.mp3" {
		t.Fatalf("listed %+v", tracks)
	}
	if artistOf(tracks[0]) != "周杰伦" || tracks[0].Duration != 269000 {
		t.Errorf("listed %+v", tracks[0])
	}
}

// A link to an audio file is one track, named and timed by its tags: only
// the start of it is read to know that.
func TestLinkTrackTags(t *testing.T) {
	body := string(id3v23([][2]string{
		{"TIT2", "海阔天空"},
		{"TPE1", "Beyond"},
		{"TALB", "乐与怒"},
		{"TLEN", "326000"},
	})) + "\xff\xfb\x90\x00"
	srv := linkServer(t, "audio/mpeg", body)

	tracks, err := Link(srv.URL + "/song.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("listed %d tracks", len(tracks))
	}
	tr := tracks[0]
	if tr.Name != "海阔天空" || artistOf(tr) != "Beyond" || tr.Album.Name != "乐与怒" {
		t.Errorf("listed %+v", tr)
	}
	if tr.Duration != 326000 {
		t.Errorf("duration %d ms", tr.Duration)
	}
	if tr.PlayURL != srv.URL+"/song.mp3" {
		t.Errorf("plays from %q", tr.PlayURL)
	}

	// A file with no tags is named after its address.
	srv2 := linkServer(t, "audio/mpeg", "no tags here")
	tracks, err = Link(srv2.URL + "/My%20Song.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if tracks[0].Name != "My Song" {
		t.Errorf("an untagged file is named %q", tracks[0].Name)
	}
}

// A link that is none of them says so.
func TestLinkUnknown(t *testing.T) {
	srv := linkServer(t, "text/html", "<html>hello</html>")
	if _, err := Link(srv.URL + "/page"); err == nil {
		t.Fatal("a page was listed as music")
	}
	if _, err := Link("ftp://example.com/a.mp3"); err == nil {
		t.Error("a link that is not http was listed")
	}
}

// Several sources list at once: one that fails leaves the others
// standing, each track names the source it came from, and a track two
// sources both list is listed once.
func TestLoadMergesSources(t *testing.T) {
	good := linkServer(t, "application/json", `[{"name":"晴天","url":"http://x/a.mp3"},{"name":"七里香","url":"http://x/b.mp3"}]`)
	also := linkServer(t, "application/json", `[{"name":"晴天","url":"http://x/a.mp3"}]`)
	broken := linkServer(t, "text/plain", "nope")

	tracks, failed := Load([]Entry{
		{ID: "s1", Name: "主接口", Kind: KindAPI, Value: good.URL, Enabled: true},
		{ID: "s2", Name: "备用", Kind: KindAPI, Value: broken.URL, Enabled: true},
		{ID: "s3", Name: "再来一份", Kind: KindAPI, Value: also.URL, Enabled: true},
		{ID: "s4", Name: "关着的", Kind: KindAPI, Value: good.URL, Enabled: false},
	})

	if len(tracks) != 2 {
		t.Fatalf("listed %d tracks: %+v", len(tracks), tracks)
	}
	if tracks[0].Source != "主接口" || tracks[1].Source != "主接口" {
		t.Errorf("tracks name their sources as %q and %q", tracks[0].Source, tracks[1].Source)
	}
	if len(failed) != 1 || !strings.HasPrefix(failed[0], "备用：") {
		t.Errorf("failures %q", failed)
	}
}

// An entry that names nothing lists nothing, and is no failure.
func TestLoadSkipsEmpty(t *testing.T) {
	tracks, failed := Load([]Entry{
		{ID: "s1", Kind: KindFolder, Value: "", Enabled: true},
		{ID: "s2", Kind: KindAPI, Value: "http://127.0.0.1:9/songs", Enabled: false},
		{ID: "s3", Kind: "subsonic", Value: "http://x", Enabled: true},
	})
	if len(tracks) != 0 || len(failed) != 0 {
		t.Errorf("listed %+v, failed %q", tracks, failed)
	}
}

// id3v23 writes an ID3v2.3 header holding the text frames given, in the
// order given.
func id3v23(frames [][2]string) []byte {
	var body []byte
	for _, f := range frames {
		payload := append([]byte{3}, []byte(f[1])...) // 3 is UTF-8.
		frame := make([]byte, 10)
		copy(frame, f[0])
		binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
		body = append(append(body, frame...), payload...)
	}
	header := []byte{'I', 'D', '3', 3, 0, 0}
	header = append(header, synchsafeOf(len(body))...)
	return append(header, body...)
}

// synchsafeOf writes n the way an ID3 header does: seven bits a byte.
func synchsafeOf(n int) []byte {
	return []byte{byte(n>>21) & 0x7f, byte(n>>14) & 0x7f, byte(n>>7) & 0x7f, byte(n) & 0x7f}
}

// A Luoxue (LX) audio-source script is JavaScript this app does not run.
// Filled in as a link or as an interface it is answered with what to do
// about it — the bridge — rather than with the format it is not, which
// would leave the listener with nothing to try.
func TestLuoxueScriptIsNamed(t *testing.T) {
	script := "'use strict'\n" +
		"const { EVENT_NAMES, request, on, send, env, version } = globalThis.lx\n" +
		"on(EVENT_NAMES.request, ({ action, source, info }) => {})\n" +
		"send(EVENT_NAMES.inited, { status: true, sources: {} })\n"
	srv := linkServer(t, "application/javascript", script)

	for _, said := range []struct {
		what string
		err  error
	}{
		{"链接", errOf(Link(srv.URL + "/HYWmusic.js"))},
		{"接口", errOf(API(srv.URL + "/HYWmusic.js"))},
	} {
		if said.err == nil {
			t.Errorf("%s：没有报错", said.what)
			continue
		}
		if !strings.Contains(said.err.Error(), "洛雪") || !strings.Contains(said.err.Error(), "音源桥") {
			t.Errorf("%s 说的是 %q，没有把人指到桥上", said.what, said.err)
		}
	}
}

// errOf is what a listing returned, for the table above.
func errOf(_ []netease.Track, err error) error { return err }
