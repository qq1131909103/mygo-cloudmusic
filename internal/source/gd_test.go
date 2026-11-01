package source

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// gdServer stands in for GD 音乐台: a search that lists songs with the ids
// of their stream, and the calls that give the stream and the cover.
func gdServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		q := r.URL.Query()
		switch q.Get("types") {
		case "search":
			w.Write([]byte(`[
				{"id":"1","name":"海阔天空","artist":["Beyond"],"album":"乐与怒","pic_id":"p1","url_id":"u1","source":"netease"},
				{"id":"2","name":"光辉岁月","artist":["Beyond"],"album":"命运派对","pic_id":"p2","url_id":"u2","source":"netease"}
			]`))
		case "url":
			if q.Get("id") == "u2" {
				w.Write([]byte(`{"url":""}`))
				return
			}
			w.Write([]byte(`{"url":"http://cdn/` + q.Get("id") + `.mp3","br":320}`))
		case "pic":
			w.Write([]byte(`{"url":"http://pic/` + q.Get("id") + `.jpg"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A GD 音乐台 search lists its songs, each asked for by the id it names.
func TestAPIGD(t *testing.T) {
	srv := gdServer(t)

	tracks, err := API(srv.URL + "/api.php?types=search&source=netease&name=%E6%B5%B7%E9%98%94%E5%A4%A9%E7%A9%BA&count=2")
	if err != nil {
		t.Fatal(err)
	}
	// The second song has no address, and is not listed.
	if len(tracks) != 1 {
		t.Fatalf("listed %d tracks: %+v", len(tracks), tracks)
	}
	tr := tracks[0]
	if tr.Name != "海阔天空" || artistOf(tr) != "Beyond" || tr.Album.Name != "乐与怒" {
		t.Errorf("listed %+v", tr)
	}
	if tr.PlayURL != "http://cdn/u1.mp3" {
		t.Errorf("plays from %q", tr.PlayURL)
	}
	if tr.Album.PicURL != "http://pic/p1.jpg" {
		t.Errorf("cover %q", tr.Album.PicURL)
	}
	if tr.ID >= 0 {
		t.Errorf("id %d is not one of the app's own", tr.ID)
	}
}

// A GD 音乐台 search is also read when it is pasted as a link.
func TestLinkGD(t *testing.T) {
	srv := gdServer(t)
	tracks, err := Link(srv.URL + "/api.php?types=search&source=netease&name=x")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].PlayURL != "http://cdn/u1.mp3" {
		t.Fatalf("listed %+v", tracks)
	}
}

// Nothing playable is said, not listed as nothing.
func TestAPIGDNoURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.URL.Query().Get("types") == "search" {
			w.Write([]byte(`[{"id":"1","name":"x","url_id":"u1","source":"netease"}]`))
			return
		}
		w.Write([]byte(`{"url":""}`))
	}))
	defer srv.Close()

	_, err := API(srv.URL + "/api.php?types=search&source=netease&name=x")
	if err == nil || !strings.Contains(err.Error(), "播放地址") {
		t.Fatalf("error %v", err)
	}
}

// within runs every index once, whatever the worker count.
func TestWithin(t *testing.T) {
	for _, workers := range []int{0, 1, 3, 10} {
		seen := make([]int, 7)
		within(len(seen), workers, func(i int) { seen[i]++ })
		for i, n := range seen {
			if n != 1 {
				t.Errorf("workers %d: index %d ran %d times", workers, i, n)
			}
		}
	}
}
