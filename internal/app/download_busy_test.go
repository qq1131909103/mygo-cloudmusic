package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// One song being saved leaves every other row's arrow live: a press on one
// of them says the app is busy, rather than being greyed out with nothing
// to say. Before this, a single slow source left the download buttons of
// every list dead until it came back.
func TestOtherRowsDownloadWhileOneIsSaving(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.downloads = &downloadState{dir: t.TempDir()}
	a.fallbackSources = &fallbackSet{}
	a.daily = []netease.Track{
		{ID: 201, Name: "甲歌", Duration: 200000},
		{ID: 202, Name: "乙歌", Duration: 200000},
	}
	a.route = route{page: pageDaily}
	// One song still being looked for, as a slow source leaves it.
	a.downloads.current = &songDownload{
		Track: netease.Track{ID: 999, Name: "慢歌"},
		Level: "exhigh",
		Stage: "正在找音源…",
	}

	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	tt.Advance(0)
	tt.Frame()
	r, ok := tt.Find("乙歌")
	if !ok {
		t.Fatal("the row is not there")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("下载"); err != nil {
		t.Fatalf("the row's download button does nothing: %v", err)
	}
	tt.Frame()
	// The press reached the app: the song being saved is still the one in
	// flight, and nothing else was started over it.
	cur, _ := a.saving()
	if cur == nil || cur.Track.ID != 999 {
		t.Errorf("the song being saved became %v", cur)
	}
}

// The song being saved says what it is doing while it has no bytes to
// count: finding it on a source takes seconds, and a button that says
// nothing through them reads as one that did nothing.
func TestSavingButtonSaysTheStage(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.downloads = &downloadState{dir: t.TempDir()}
	a.fallbackSources = &fallbackSet{}
	a.daily = []netease.Track{{ID: 201, Name: "甲歌", Duration: 200000}}
	a.route = route{page: pageDaily}
	a.downloads.current = &songDownload{
		Track: netease.Track{ID: 201, Name: "甲歌"},
		Level: "exhigh",
		Stage: "正在找音源…",
	}
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	tt.Advance(0)
	tt.Frame()
	if !tt.HasText("正在找音源…") {
		t.Errorf("the saving button says nothing about it: %v", tt.Texts())
	}
}
