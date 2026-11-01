package source

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mirror stands in for a NeteaseCloudMusicApi deployment: a search that
// answers songs without addresses, and the call that gives the addresses.
func mirror(t *testing.T, urlAnswer, v1Answer string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/search":
			w.Write([]byte(`{"code":200,"result":{"songs":[
				{"id":1,"name":"晴天","ar":[{"name":"周杰伦"}],"al":{"name":"叶惠美","picUrl":"http://pic/1.jpg"},"dt":269000},
				{"id":2,"name":"七里香","ar":[{"name":"周杰伦"}],"al":{"name":"七里香"},"dt":299000}
			]}}`))
		case "/playlist/detail":
			w.Write([]byte(`{"code":200,"playlist":{"name":"周杰伦","tracks":[
				{"id":1,"name":"晴天","ar":[{"name":"周杰伦"}],"al":{"name":"叶惠美"},"dt":269000}
			]}}`))
		case "/song/url":
			w.Write([]byte(urlAnswer))
		case "/song/url/v1":
			w.Write([]byte(v1Answer))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"code":404}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A search of a mirror lists its songs, with the addresses the mirror
// gives for them: one call for the whole list.
func TestAPINeteaseMirror(t *testing.T) {
	srv := mirror(t,
		`{"code":200,"data":[{"id":1,"url":"http://cdn/1.mp3"},{"id":2,"url":null}]}`,
		`{"code":200,"data":[]}`)

	tracks, err := API(srv.URL + "/search?keywords=%E5%91%A8%E6%9D%B0%E4%BC%A6&limit=2")
	if err != nil {
		t.Fatal(err)
	}
	// The second song has no address, and is not listed.
	if len(tracks) != 1 {
		t.Fatalf("listed %d tracks: %+v", len(tracks), tracks)
	}
	tr := tracks[0]
	if tr.Name != "晴天" || artistOf(tr) != "周杰伦" || tr.Album.Name != "叶惠美" {
		t.Errorf("listed %+v", tr)
	}
	if tr.Duration != 269000 || tr.PlayURL != "http://cdn/1.mp3" {
		t.Errorf("listed %d ms at %q", tr.Duration, tr.PlayURL)
	}
	if tr.Album.PicURL != "http://pic/1.jpg" {
		t.Errorf("cover %q", tr.Album.PicURL)
	}
	if tr.ID >= 0 {
		t.Errorf("id %d is not one of the app's own", tr.ID)
	}
}

// A mirror that answers the plain address call empty is asked the newer
// one, which names a level.
func TestAPINeteaseMirrorFallsBackToV1(t *testing.T) {
	srv := mirror(t,
		`{"code":200,"data":[{"id":1,"url":null},{"id":2,"url":null}]}`,
		`{"code":200,"data":[{"id":1,"url":"http://cdn/1b.mp3"},{"id":2,"url":"http://cdn/2b.mp3"}]}`)

	tracks, err := API(srv.URL + "/search?keywords=x")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 || tracks[0].PlayURL != "http://cdn/1b.mp3" {
		t.Fatalf("listed %+v", tracks)
	}
}

// A playlist detail of a mirror lists its songs the same way.
func TestAPINeteasePlaylist(t *testing.T) {
	srv := mirror(t,
		`{"code":200,"data":[{"id":1,"url":"http://cdn/1.mp3"}]}`,
		`{"code":200,"data":[]}`)

	tracks, err := API(srv.URL + "/playlist/detail?id=9")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].PlayURL != "http://cdn/1.mp3" {
		t.Fatalf("listed %+v", tracks)
	}
}

// A mirror that gives no addresses says so, rather than listing nothing.
func TestAPINeteaseNoURLs(t *testing.T) {
	srv := mirror(t, `{"code":200,"data":[]}`, `{"code":200,"data":[]}`)
	_, err := API(srv.URL + "/search?keywords=x")
	if err == nil || !strings.Contains(err.Error(), "播放地址") {
		t.Fatalf("error %v", err)
	}
}

// A link to a mirror works as an endpoint does.
func TestLinkNeteaseMirror(t *testing.T) {
	srv := mirror(t,
		`{"code":200,"data":[{"id":1,"url":"http://cdn/1.mp3"}]}`,
		`{"code":200,"data":[]}`)

	tracks, err := Link(srv.URL + "/search?keywords=x")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Name != "晴天" {
		t.Fatalf("listed %+v", tracks)
	}
}

// Meting answers with the app's own shape already, `pic` where the app
// reads a cover: it lists as it is.
func TestAPIMeting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(`[{"name":"海阔天空","artist":"Beyond","url":"http://cdn/h.mp3","pic":"http://pic/h.jpg","lrc":"http://lrc/h.lrc"}]`))
	}))
	defer srv.Close()

	tracks, err := API(srv.URL + "/?type=playlist&id=1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Name != "海阔天空" || artistOf(tracks[0]) != "Beyond" {
		t.Fatalf("listed %+v", tracks)
	}
	if tracks[0].Album.PicURL != "http://pic/h.jpg" {
		t.Errorf("cover %q", tracks[0].Album.PicURL)
	}
}

// Naming a site instead of its interface says so, instead of reading a
// page as JSON and blaming the JSON.
func TestAPIHTMLSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<!doctype html><html><head><title>NeteaseCloudMusicApi</title></head><body>docs</body></html>"))
	}))
	defer srv.Close()

	_, err := API(srv.URL + "/")
	if err == nil || !strings.Contains(err.Error(), "网页") {
		t.Fatalf("error %v", err)
	}
}
