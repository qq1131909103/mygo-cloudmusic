package source

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// apiSaying is an interface that answers what it is told to, whatever it
// is asked for: the shapes below are what interfaces answer with.
func apiSaying(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Meting writes a song as "title", "author" and "url"; it is read as it
// is, without the listener having to rename anything.
func TestAPIMetingShape(t *testing.T) {
	srv := apiSaying(t, `[{"title":"海阔天空","author":"Beyond","url":"http://cdn/h.mp3",`+
		`"pic":"http://pic/h.jpg","lrc":"http://lrc/h.lrc"}]`)

	tracks, err := API(srv.URL + "/api?server=netease&type=search&id=x")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("listed %+v", tracks)
	}
	tr := tracks[0]
	if tr.Name != "海阔天空" || artistOf(tr) != "Beyond" || tr.PlayURL != "http://cdn/h.mp3" {
		t.Errorf("listed %+v", tr)
	}
	if tr.Album.PicURL != "http://pic/h.jpg" {
		t.Errorf("cover %q", tr.Album.PicURL)
	}
}

// A song written as "song", "singer" and "src" names the same things.
func TestAPIHandWrittenShape(t *testing.T) {
	srv := apiSaying(t, `{"code":200,"data":[{"song":"春日影","singer":"MyGO!!!!!","src":"http://cdn/1.mp3",`+
		`"album":"迷跡波","interval":300}]}`)

	tracks, err := API(srv.URL + "/songs")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("listed %+v", tracks)
	}
	tr := tracks[0]
	if tr.Name != "春日影" || artistOf(tr) != "MyGO!!!!!" || tr.PlayURL != "http://cdn/1.mp3" {
		t.Errorf("listed %+v", tr)
	}
	if tr.Album.Name != "迷跡波" {
		t.Errorf("album %q", tr.Album.Name)
	}
	// A length under a minute is written in seconds, as interfaces do.
	if tr.Duration != 300000 {
		t.Errorf("length %d ms", tr.Duration)
	}
}

// An answer that holds its list inside an object is read too.
func TestAPINestedList(t *testing.T) {
	srv := apiSaying(t, `{"code":200,"msg":"ok","data":{"list":[{"name":"晴天","artist":"周杰伦","url":"http://cdn/2.mp3"}]}}`)

	tracks, err := API(srv.URL + "/api/search?keyword=x")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Name != "晴天" || artistOf(tracks[0]) != "周杰伦" {
		t.Fatalf("listed %+v", tracks)
	}
}

// Who sings a track is read whether the interface wrote one name, a list
// of them, or a list of objects each naming itself.
func TestAPIArtistShapes(t *testing.T) {
	for _, c := range []struct {
		what string
		body string
		want []string
	}{
		{"one name", `[{"name":"a","artist":"Beyond","url":"http://cdn/a.mp3"}]`, []string{"Beyond"}},
		{"a list", `[{"name":"a","artists":["黄贯中","叶世荣"],"url":"http://cdn/a.mp3"}]`, []string{"黄贯中", "叶世荣"}},
		{"objects", `[{"name":"a","artists":[{"name":"黄贯中"},{"name":"叶世荣"}],"url":"http://cdn/a.mp3"}]`, []string{"黄贯中", "叶世荣"}},
	} {
		srv := apiSaying(t, c.body)
		tracks, err := API(srv.URL + "/songs")
		if err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		if len(tracks) != 1 || len(tracks[0].Artists) != len(c.want) {
			t.Fatalf("%s: listed %+v", c.what, tracks)
		}
		for i, name := range c.want {
			if tracks[0].Artists[i].Name != name {
				t.Errorf("%s: artist %d is %q, want %q", c.what, i, tracks[0].Artists[i].Name, name)
			}
		}
		srv.Close()
	}
}

// An answer that lists nothing says why when it says why: a message
// beside its error code is what tells a listener to log in or to look
// for another interface.
func TestAPIWhyItListedNothing(t *testing.T) {
	srv := apiSaying(t, `{"code":403,"msg":"请先登录，或在镜像上开启游客访问"}`)
	_, err := API(srv.URL + "/search?keywords=x")
	if err == nil || !strings.Contains(err.Error(), "请先登录") {
		t.Fatalf("error %v", err)
	}
}

// An interface that answers with a list of songs it cannot address says
// so, rather than listing songs that play from nowhere.
func TestAPITracksWithoutAddress(t *testing.T) {
	srv := apiSaying(t, `[{"title":"海阔天空","author":"Beyond"}]`)
	_, err := API(srv.URL + "/search?keywords=x")
	if err == nil || !strings.Contains(err.Error(), "没有返回可播放的歌曲") {
		t.Fatalf("error %v", err)
	}
}

// An address pasted without its scheme works as it is: what a browser
// fills in, the app fills in too.
func TestAPIWithoutScheme(t *testing.T) {
	srv := apiSaying(t, `[{"name":"晴天","artist":"周杰伦","url":"http://cdn/2.mp3"}]`)

	tracks, err := API(strings.TrimPrefix(srv.URL, "http://") + "/search?keywords=x")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Name != "晴天" {
		t.Fatalf("listed %+v", tracks)
	}
}

// What a browser shows is what people paste: a name written in Chinese,
// with a space in it. It reaches the server encoded, as it would from a
// browser, and the marks of the query are left alone.
func TestAPIEncodesWhatIsPasted(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		io.WriteString(w, `[{"name":"海阔天空","artist":"Beyond","url":"http://cdn/h.mp3"}]`)
	}))
	defer srv.Close()

	tracks, err := API(srv.URL + "/search?keywords=海阔天空&by=Beyond 黄家驹&limit=2")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("listed %+v", tracks)
	}
	if !strings.Contains(raw, "%E6%B5%B7%E9%98%94%E5%A4%A9%E7%A9%BA") {
		t.Errorf("the name reached the server as %q", raw)
	}
	if strings.Contains(raw, "%25") || !strings.Contains(raw, "&by=") || !strings.Contains(raw, "&limit=2") {
		t.Errorf("the query was written as %q", raw)
	}
}

// A playlist pasted where an interface was expected is read as what it
// holds, rather than refused over the kind it was filled in under.
func TestAPIPlaylistPastedAsInterface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		io.WriteString(w, "#EXTM3U\n#EXTINF:239,Beyond - 海阔天空\nhttp://cdn/h.mp3\n")
	}))
	defer srv.Close()

	tracks, err := API(srv.URL + "/list.m3u")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].PlayURL != "http://cdn/h.mp3" {
		t.Fatalf("listed %+v", tracks)
	}
	if tracks[0].Name != "海阔天空" || artistOf(tracks[0]) != "Beyond" {
		t.Errorf("listed %+v", tracks[0])
	}
	if tracks[0].Duration != 239000 {
		t.Errorf("Duration = %d, want 239000", tracks[0].Duration)
	}
}
