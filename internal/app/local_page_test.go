package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// localFolder is a folder of this machine's music, kept as a source.
func localFolder(name, path string) source.Entry {
	e := source.Entry{}
	e.ID = "l-" + name
	e.Name = name
	e.Kind = source.KindFolder
	e.Value = path
	e.Enabled = true
	return e
}

// one is a song of the source named, as the sources listed it.
func one(id int64, name, from string) netease.Track {
	tr := netease.Track{}
	tr.ID = id
	tr.Name = name
	tr.PlayURL = "D:/音乐/" + name + ".mp3"
	tr.Source = from
	return tr
}

// A listener with no folder of their own is offered the one button that
// makes the page useful, on the page itself.
func TestLocalPageOffersAFolder(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.open(route{page: pageLocal})
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if !tt.HasText("本地音乐") {
		t.Errorf("the page is not named: %q", tt.Texts())
	}
	if !tt.HasText("添加音乐文件夹") {
		t.Errorf("the page offers no way to add one: %q", tt.Texts())
	}
	if !tt.HasText("听这台电脑上的音乐") {
		t.Errorf("the page does not say what it is for: %q", tt.Texts())
	}
}

// With a folder kept, the page lists its music and the folder it came
// from, and still offers to add another.
func TestLocalPageListsMusic(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Sources = []source.Entry{localFolder("我的音乐", "D:/音乐")}
	a.open(route{page: pageLocal})
	a.localSongs = []netease.Track{one(1, "春日影", "我的音乐")}
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if !tt.HasText("春日影") {
		t.Errorf("the music of the folder is not listed: %q", tt.Texts())
	}
	if !tt.HasText("我的音乐") || !tt.HasText("D:/音乐") {
		t.Errorf("the folder itself is not listed: %q", tt.Texts())
	}
	if !tt.HasText("1 个文件夹 · 1 首") {
		t.Errorf("the page does not count what it holds: %q", tt.Texts())
	}
	if !tt.HasText("添加音乐文件夹") {
		t.Errorf("the page offers no way to add another: %q", tt.Texts())
	}
}

// The page over a folder reads the music of this machine, and only it:
// a song of another source is not one of this machine's.
func TestLocalPageReadsItsOwnMusic(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Sources = []source.Entry{localFolder("我的音乐", "D:/音乐")}
	a.open(route{page: pageLocal})
	a.localSongs = []netease.Track{one(1, "春日影", "我的音乐")}
	if got := len(a.localTracks()); got != 1 {
		t.Errorf("the local page reads %d songs", got)
	}
	entries := a.localEntries()
	if len(entries) != 1 || entries[0].Title() != "我的音乐" {
		t.Errorf("the local page reads %+v", entries)
	}
	a.localSongs = []netease.Track{one(1, "春日影", "我的音乐")}
	songs := a.localTracks()
	if len(songs) != 1 || songs[0].Name != "春日影" {
		t.Errorf("the local page reads %+v", songs)
	}
}
