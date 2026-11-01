package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// A song NetEase charges for says so whether or not it can be played: a
// listener who keeps a source plays it, and still wants to know which
// songs are the paid ones. Before this, the mark went as soon as a source
// could stand in for the song, and nothing said it was paid.
func TestTrackMarkForPaid(t *testing.T) {
	theme := ui.Theme{}
	p := paletteFor(&theme)
	paid := memberTrack()
	free := netease.Track{ID: 2, Name: "晴天", Fee: 0}

	// With nothing to stand in for it, a paid song cannot play, and is
	// marked all the same.
	if mark, _ := trackMark(&paid, false, p); mark != "VIP" {
		t.Errorf("a paid song that cannot play is marked %q, want VIP", mark)
	}
	// And with a source kept it can: the mark stays.
	if mark, _ := trackMark(&paid, true, p); mark != "VIP" {
		t.Errorf("a paid song a source plays is marked %q, want VIP", mark)
	}
	// A free song is marked with nothing at all.
	if mark, _ := trackMark(&free, true, p); mark != "" {
		t.Errorf("a free song is marked %q, want nothing", mark)
	}
	// One there is nothing to play from, which is not paid for either.
	broken := netease.Track{ID: 3, Name: "无", Fee: 0}
	if mark, _ := trackMark(&broken, false, p); mark != "不可播" {
		t.Errorf("a song with nothing to play it is marked %q, want 不可播", mark)
	}
	// One NetEase has withdrawn.
	gone := netease.Track{ID: 4, Name: "下架", Privilege: &netease.Privilege{St: -200, Pl: 128000}}
	if mark, _ := trackMark(&gone, false, p); mark != "已下架" {
		t.Errorf("a withdrawn song is marked %q, want 已下架", mark)
	}
}

// The lists show the mark on the row of a paid song, whether or not a
// source makes it play: what is paid for is told apart from what is free.
func TestPaidSongIsMarkedInAList(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	keepSources(a)
	a.playlists[7] = &netease.Playlist{ID: 7, Name: "歌单", Tracks: []netease.Track{
		memberTrack(),
		{ID: 2, Name: "晴天", Fee: 0},
	}}
	a.route = route{page: pagePlaylist, id: 7}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("VIP") {
		t.Errorf("the paid song is not marked: %q", tt.Texts())
	}

	// With a source that answers for it, it plays, and the mark stays.
	keepSources(a, scriptSource(true))
	tt.Frame()
	if !tt.HasText("VIP") {
		t.Errorf("the paid song lost its mark once a source could play it: %q", tt.Texts())
	}
	if !tt.HasText("晴天") {
		t.Errorf("the free song is missing from the list: %q", tt.Texts())
	}
}

// The mark is on Now Playing too, beside the name of the song, so that
// what plays is told apart from what is free wherever it is seen.
func TestPaidSongIsMarkedWhilePlaying(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	keepSources(a)
	a.pl.Cue([]netease.Track{memberTrack()})
	a.pl.SetLyrics(nil)
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(700 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("VIP") {
		t.Errorf("the song playing is not marked: %q", tt.Texts())
	}
}

// A song of the listener's own is nobody's to charge for, and the mark is
// NetEase's: a fee carried beside a third-party track says nothing, so a
// file of this machine is not marked as a paid one.
func TestOwnMusicIsNotMarkedPaid(t *testing.T) {
	theme := ui.Theme{}
	p := paletteFor(&theme)
	own := netease.Track{ID: -1, Name: "春日影", Fee: 1, PlayURL: "D:/音乐/春日影.mp3"}
	if mark, _ := trackMark(&own, true, p); mark != "" {
		t.Errorf("a file of this machine is marked %q; the fee is NetEase`s, and this is not NetEase`s", mark)
	}
}
