package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// playlistRows is a playlist of the songs given, open on its page.
func playlistRows(t *testing.T, a *app, songs ...netease.Track) ui.Tester {
	t.Helper()
	a.playlists[7] = &netease.Playlist{ID: 7, Name: "歌单", Tracks: songs}
	a.route = route{page: pagePlaylist, id: 7}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(0)
	tt.Frame()
	return *tt
}

// The download arrow on a row of a list saves that row's song at the best
// a source can give, in one press: no menu stands between, because a menu
// opened from inside a list does not keep the press — it lands on the row
// under it, which plays that row instead.
func TestListDownloadButtonSavesItsOwnRow(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.downloads = &downloadState{dir: t.TempDir()}
	a.fallbackSources = &fallbackSet{}
	tt := playlistRows(t, a,
		netease.Track{ID: 1, Name: "第一首", Duration: 200000},
		netease.Track{ID: 2, Name: "第二首", Duration: 200000},
	)

	r, ok := tt.Find("第二首")
	if !ok {
		t.Fatal("the row is not there")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("下载"); err != nil {
		t.Fatalf("click 下载: %v", err)
	}
	tt.Frame()
	cur, _ := a.saving()
	if cur == nil {
		t.Fatal("the press saved nothing")
	}
	if cur.Track.Name != "第二首" {
		t.Errorf("saving %q, want the row it was on, 第二首", cur.Track.Name)
	}
	if cur.Level != "best" {
		t.Errorf("saving at %q, want the best a source can give", cur.Level)
	}
}

// Pressing the download arrow on a row saves that song rather than playing
// it: the row itself is a button, and the click must not reach it.
func TestListDownloadDoesNotPlayTheRow(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.downloads = &downloadState{dir: t.TempDir()}
	a.fallbackSources = &fallbackSet{}
	tt := playlistRows(t, a,
		netease.Track{ID: 1, Name: "第一首", Duration: 200000},
		netease.Track{ID: 2, Name: "第二首", Duration: 200000},
	)

	r, ok := tt.Find("第二首")
	if !ok {
		t.Fatal("the row is not there")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("下载"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if cur := a.pl.Current(); cur != nil {
		t.Errorf("pressing download played %q; the click reached the row", cur.Name)
	}
}

// Two rows carry two download buttons, each keyed by its own song: two
// elements sharing a key would share one state, and only one would work.
func TestRowsHaveTheirOwnDownloadButtons(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.downloads = &downloadState{dir: t.TempDir()}
	a.fallbackSources = &fallbackSet{}
	tt := playlistRows(t, a,
		netease.Track{ID: 11, Name: "第一首", Duration: 200000},
		netease.Track{ID: 22, Name: "第二首", Duration: 200000},
	)

	// Each row saves its own song, one after the other.
	for _, c := range []struct {
		name string
		id   int64
	}{
		{"第一首", 11},
		{"第二首", 22},
	} {
		r, ok := tt.Find(c.name)
		if !ok {
			t.Fatalf("the row %q is not there", c.name)
		}
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		tt.Frame()
		if err := tt.Click("下载"); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		tt.Frame()
		cur, _ := a.saving()
		if cur == nil {
			t.Fatalf("%s: no download started", c.name)
		}
		if cur.Track.ID != c.id {
			t.Errorf("%s: saving the song %d, want %d", c.name, cur.Track.ID, c.id)
		}
		// Shut the first one down so the next can start.
		a.downloads.mu.Lock()
		a.downloads.current, a.downloads.last = nil, nil
		a.downloads.mu.Unlock()
		tt.Frame()
	}
}
