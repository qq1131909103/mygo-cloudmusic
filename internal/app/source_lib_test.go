package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// sourceSong is a song of a third-party source, as the sources list one.
func sourceSong(id int64, name, artist, from string) netease.Track {
	tr := netease.Track{ID: id, Name: name, Source: from}
	tr.PlayURL = "D:/music/" + name + "-" + from + ".mp3"
	tr.Artists = []netease.Artist{{Name: artist}}
	return tr
}

// libEntry is one source the settings keep.
func libEntry(id, name, kind, value string, on bool) source.Entry {
	return source.Entry{ID: id, Name: name, Kind: kind, Value: value, Enabled: on}
}

// A searchable source is one whose address carries the search mark: it is
// asked for the words. A folder lists this machine, and a script answers
// for one song at a time by its NetEase id, so neither can be searched.
func TestSourceSearchable(t *testing.T) {
	for _, c := range []struct {
		name  string
		entry source.Entry
		want  bool
	}{
		{"an address that searches", libEntry("s", "接口", source.KindAPI, "http://x/s?name={keyword}", true), true},
		{"a plain endpoint", libEntry("s", "接口", source.KindAPI, "http://x/list", true), false},
		{"a folder", libEntry("s", "本机", source.KindFolder, "C:/music", true), false},
		{"a script", libEntry("s", "脚本", source.KindScript, "C:/m/hyw.js", true), false},
		{"one that is off", libEntry("s", "关掉的", source.KindAPI, "http://x/s?w={keyword}", false), false},
	} {
		if got := c.entry.Searchable(); got != c.want {
			t.Errorf("%s: searchable %v, want %v", c.name, got, c.want)
		}
	}
}

// Searching the sources asks the interfaces that can be searched for the
// words, and shows what they answer with: the songs come from those
// interfaces, not from the songs the sources already listed. Before this,
// the field filtered the listed songs, so searching it found the music on
// this machine.
func TestSourceSearchAsksTheInterfaces(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"name": "网上搜到的歌", "artist": "网上歌手", "url": "https://cdn.test/a.mp3"},
			{"name": "网上第二首", "artist": "网上歌手", "url": "https://cdn.test/b.mp3"}
		]`))
	}))
	defer srv.Close()

	a := newTestApp()
	a.settings = defaultSettings()
	// A folder of this machine is listed, and must not be what a search
	// answers from.
	a.settings.Sources = []source.Entry{
		libEntry("s1", "接口", source.KindAPI, srv.URL+"/search?name={keyword}", true),
		libEntry("s2", "本机", source.KindFolder, "C:/music", true),
	}
	a.sourceTracks = []netease.Track{sourceSong(1, "本机里的歌", "本机歌手", "本机")}

	// Asked straight, as the page asks it: what the interfaces answer is
	// what the search shows.
	found, failed := source.Search(a.searchableEntries(), "海阔天空")
	if len(failed) != 0 {
		t.Errorf("the sources failed: %v", failed)
	}
	if asked == "" {
		t.Fatal("the interface was never asked")
	}
	if !strings.Contains(asked, "name=%E6%B5%B7%E9%98%94%E5%A4%A9%E7%A9%BA") {
		t.Errorf("the interface was asked %q, without the words escaped", asked)
	}
	if len(found) != 2 {
		t.Fatalf("the search found %d songs, want 2, from the interface", len(found))
	}
	if found[0].Name != "网上搜到的歌" {
		t.Errorf("the first is %q, want what the interface answered", found[0].Name)
	}
	for _, tr := range found {
		if tr.Name == "本机里的歌" {
			t.Error("the search answered with the music on this machine")
		}
		if tr.Source != "接口" {
			t.Errorf("%q came from %q, want the interface that answered", tr.Name, tr.Source)
		}
	}
}

// A search with nothing to ask says so, rather than looking as if it found
// nothing: only an address carrying the search mark can be searched.
func TestSourceSearchWithoutASearchableSource(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Sources = []source.Entry{
		libEntry("s1", "脚本", source.KindScript, "C:/m/hyw.js", true),
	}
	if source.Searchable(a.settings.Sources) {
		t.Error("a script counts as a source that can be searched")
	}
	if n := len(a.searchableEntries()); n != 0 {
		t.Errorf("%d sources can be searched, want none", n)
	}
	found, _ := source.Search(a.searchableEntries(), "海阔天空")
	if len(found) != 0 {
		t.Errorf("it found %v with nothing to ask", names(found))
	}
}

// The songs may be sorted by name, by singer, or by the source they came
// from, which is what a library of several sources needs.
func TestSourceSort(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.sourceTracks = []netease.Track{
		sourceSong(1, "海阔天空", "Beyond", "HYW"),
		sourceSong(2, "晴天", "周杰伦", "HYW"),
		sourceSong(3, "春日影", "MyGO", "我的音乐"),
	}

	for _, c := range []struct {
		order string
		first string
	}{
		{"", "海阔天空"}, // the order the sources listed them in
		// By name, as a reader of the language sorts: 春日影 (chun) before
		// 海阔天空 (hai) before 晴天 (qing).
		{"name", "春日影"},
		{"artist", "海阔天空"}, // Beyond before MyGO
		{"source", "海阔天空"}, // HYW before 我的音乐
	} {
		a.sourceView.order = c.order
		got := a.sourceShown()
		if len(got) == 0 || got[0].Name != c.first {
			t.Errorf("sorted by %q it starts at %v, want %q", c.order, names(got), c.first)
		}
	}
}

// names is the titles of tracks, for a test to say what it found.
func names(tracks []netease.Track) []string {
	out := make([]string, len(tracks))
	for i, t := range tracks {
		out[i] = t.Name
	}
	return out
}
