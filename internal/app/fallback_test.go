package app

import (
	"testing"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// memberTrack is a song NetEase refuses: it asks for a membership.
func memberTrack() netease.Track {
	tr := netease.Track{}
	tr.ID = 347230
	tr.Name = "海阔天空"
	tr.Fee = 1
	tr.Privilege = &netease.Privilege{}
	tr.Artists = []netease.Artist{{Name: "Beyond"}}
	return tr
}

// keepSources gives a the sources listed, the way the settings do.
func keepSources(a *app, entries ...source.Entry) {
	if a.fallbackSources == nil {
		a.fallbackSources = &fallbackSet{}
	}
	a.fallbackSources.keep(entries)
}

// scriptSource is a source script kept on this machine, which is what a
// song NetEase will not serve falls back on.
func scriptSource(on bool) source.Entry {
	e := source.Entry{}
	e.ID = "s1"
	e.Kind = source.KindScript
	e.Value = "/music/HYWmusic.js"
	e.Enabled = on
	return e
}

// A song NetEase will not serve plays when a source the listener keeps
// can find where it plays instead: a source script, or an address that
// takes the words to search for. Without one it is what it always was.
func TestCanPlayWithSources(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	keepSources(a)

	tr := memberTrack()
	if tr.Playable() {
		t.Fatal("the song is meant to be one NetEase will not serve")
	}
	if a.canPlay(&tr) {
		t.Error("with no sources kept, the song is offered anyway")
	}

	keepSources(a, scriptSource(true))
	if !a.canPlay(&tr) {
		t.Error("with a source script kept, the song is still refused")
	}

	search := source.Entry{}
	search.ID = "s1"
	search.Kind = source.KindAPI
	search.Value = "http://127.0.0.1:8720/songs?keyword=" + source.SearchMark
	search.Enabled = true
	keepSources(a, search)
	if !a.canPlay(&tr) {
		t.Error("with an address that searches kept, the song is still refused")
	}

	// A source that only lists its own songs has nothing to ask about
	// one particular song, and is passed over.
	listing := source.Entry{}
	listing.ID = "s1"
	listing.Kind = source.KindAPI
	listing.Value = "http://127.0.0.1:8720/songs?preset=beyond"
	listing.Enabled = true
	folder := source.Entry{}
	folder.ID = "s2"
	folder.Kind = source.KindFolder
	folder.Value = "/music"
	folder.Enabled = true
	keepSources(a, listing, folder)
	if a.canPlay(&tr) {
		t.Error("a source that only lists songs is taken for one that answers")
	}

	// Neither does one that is turned off.
	keepSources(a, scriptSource(false))
	if a.canPlay(&tr) {
		t.Error("a source that is off answers for a song")
	}

	// An episode brings its own audio, which the state it carries
	// already settles: a source of songs has nothing to do with it.
	ep := memberTrack()
	ep.ProgramID = 9
	keepSources(a, scriptSource(true))
	if !a.canPlay(&ep) {
		t.Error("an episode that streams became unplayable")
	}
}

// A list starts at a song that plays, which with a source kept includes
// the songs NetEase would not have played.
func TestFirstPlayableFor(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	keepSources(a)

	only := []netease.Track{memberTrack()}
	if got := a.firstPlayableFor(only); got != 0 {
		t.Errorf("with no sources it starts at %d, want 0", got)
	}
	keepSources(a, scriptSource(true))
	if got := a.firstPlayableFor(only); got != 0 {
		t.Errorf("with a source script it starts at %d, want 0", got)
	}

	free := netease.Track{}
	free.ID = 1
	free.Name = "晴天"
	both := []netease.Track{free, memberTrack()}
	if got := a.firstPlayableFor(both); got != 0 {
		t.Errorf("it starts at %d, want the first song, 0", got)
	}
	if got := firstPlayable(only); got != 0 {
		t.Errorf("firstPlayable returned %d", got)
	}
}

// What the listener keeps decides whether there is anything to answer for
// a song NetEase will not serve at all.
func TestAnswersSongs(t *testing.T) {
	if source.AnswersSongs(nil) {
		t.Error("keeping nothing counts as something that answers")
	}
	if !source.AnswersSongs([]source.Entry{scriptSource(true)}) {
		t.Error("a source script does not count as one that answers")
	}
	if source.AnswersSongs([]source.Entry{scriptSource(false)}) {
		t.Error("a source that is off counts as one that answers")
	}
	if source.AnswersSongs([]source.Entry{source.Entry{ID: "f", Kind: source.KindFolder, Value: "/music", Enabled: true}}) {
		t.Error("a folder counts as something that answers for a song")
	}
}
