package app

import (
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// sourceEntry is one source the settings keep.
func sourceEntry(id, kind, value string) source.Entry {
	return source.Entry{ID: id, Kind: kind, Value: value, Enabled: true}
}

// sourceTrack is a song a source lists, playing from a file of its own.
func sourceTrack(path string) netease.Track {
	tr := netease.Track{ID: source.ID(path), Name: "春日影", PlayURL: path}
	tr.Artists = []netease.Artist{{Name: "MyGO!!!!!"}}
	return tr
}

// askedSources replaces listing one source with one that reports what it
// was asked, and answers with a song of its own.
func askedSources(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	before := listOne
	listOne = func(e source.Entry) ([]netease.Track, error) {
		mu.Lock()
		asked = append(asked, e.Kind+"|"+e.Value)
		mu.Unlock()
		return []netease.Track{sourceTrack("/music/春日影.mp3")}, nil
	}
	t.Cleanup(func() { listOne = before })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := append([]string(nil), asked...)
		sort.Strings(out)
		asked = nil
		return out
	}
}

// waitFor waits for the sources asked to settle at want, and fails with
// what they were.
func waitFor(t *testing.T, taken func() []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := taken()
		if strings.Join(got, ",") == strings.Join(want, ",") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("asked %q, want %q", got, want)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The sources are listed once, and listed again when they are asked for
// afresh: as a refresh does, or as the settings name others.
func TestLoadSourceListsWhenAsked(t *testing.T) {
	taken := askedSources(t)

	a := newTestApp()
	a.settings = defaultSettings()
	// No source lists nothing, and asks for nothing.
	a.loadSource(false)
	a.loadSource(false)
	time.Sleep(30 * time.Millisecond)
	if got := taken(); len(got) != 0 {
		t.Fatalf("no source asked for %q", got)
	}

	a.settings.Sources = []source.Entry{sourceEntry("s1", source.KindFolder, "/music")}
	a.loadSource(false)
	waitFor(t, taken, "folder|/music")

	// Listed already: opening the page again lists nothing again.
	a.loadSource(false)
	time.Sleep(30 * time.Millisecond)
	if got := taken(); len(got) != 0 {
		t.Fatalf("the same source was listed again: %q", got)
	}

	a.loadSource(true)
	waitFor(t, taken, "folder|/music")

	// The settings naming another source list both.
	a.settings.Sources = append(a.settings.Sources, sourceEntry("s2", source.KindAPI, "http://127.0.0.1:8080/songs"))
	a.reloadSource()
	waitFor(t, taken, "folder|/music", "api|http://127.0.0.1:8080/songs")

	// The settings naming what they named before: nothing to list anew.
	a.reloadSource()
	time.Sleep(30 * time.Millisecond)
	if got := taken(); len(got) != 0 {
		t.Fatalf("unchanged sources were listed again: %q", got)
	}

	// A source that is turned off is left out, and naming it off lists
	// what is left again.
	a.settings.Sources[1].Enabled = false
	a.reloadSource()
	waitFor(t, taken, "folder|/music")
}

// What a listing lands with fills the page, unless other sources were
// asked for meanwhile.
func TestSourceLanded(t *testing.T) {
	tr := sourceTrack("/music/春日影.mp3")
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Sources = []source.Entry{sourceEntry("s1", source.KindFolder, "/music")}
	a.loadSource(false)
	a.sourceLoading = true
	a.sourceLanded(a.sourceGen, []netease.Track{tr}, nil)
	if len(a.sourceTracks) != 1 || a.sourceTracks[0].Name != tr.Name {
		t.Errorf("the listing landed with %+v", a.sourceTracks)
	}
	if a.sourceLoading {
		t.Error("the page is still said to be loading")
	}
	// A listing of another set of sources lands nowhere.
	a.sourceGen++
	a.sourceLanded(a.sourceGen-1, nil, nil)
	if len(a.sourceTracks) != 1 {
		t.Error("a stale listing replaced what is shown")
	}
}
