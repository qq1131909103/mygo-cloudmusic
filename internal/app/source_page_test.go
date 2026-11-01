package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// A track of a source, named after the file it plays from.
func pageTrack(id int64, name, artist string) netease.Track {
	tr := netease.Track{}
	tr.ID = id
	tr.Name = name
	tr.PlayURL = "C:/music/" + name + ".mp3"
	tr.Artists = []netease.Artist{{Name: artist}}
	return tr
}

// A scripted source, kept on this machine.
func pageScript(name, value string, on bool) source.Entry {
	e := source.Entry{}
	e.ID = "s-" + name
	e.Name = name
	e.Kind = source.KindScript
	e.Value = value
	e.Enabled = on
	return e
}

// The page of the sources lists the sources themselves under the songs
// they give: what each is called, what kind it is, and what it answered
// when it was tested. A source that lists nothing - a script - is still
// named, so that the page says where its music comes from.
func TestSourcePageListsSources(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Sources = append(a.settings.Sources, pageScript("HYWmusic", "C:/music/HYWmusic.js", true))
	folder := source.Entry{}
	folder.ID = "s-folder"
	folder.Name = "我的音乐"
	folder.Kind = source.KindFolder
	folder.Value = "C:/music"
	folder.Enabled = true
	a.settings.Sources = append(a.settings.Sources, folder)
	a.sourceTests = map[string]string{}
	a.setSourceTest(a.settings.Sources[0], "可以，这是一个洛雪音源脚本，支持 9 个平台")

	a.open(route{page: pageSource})
	a.sourceLanded(a.sourceGen, []netease.Track{pageTrack(1, "海阔天空", "Beyond")}, nil)
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()

	if !tt.HasText("海阔天空") {
		t.Errorf("the songs the sources give are not listed: %q", tt.Texts())
	}
	if !tt.HasText("HYWmusic") || !tt.HasText("我的音乐") {
		t.Errorf("the sources themselves are not listed: %q", tt.Texts())
	}
	if !tt.HasText("音源接口") || !tt.HasText("脚本：C:/music/HYWmusic.js") {
		t.Errorf("the sources interfaces are not shown: %q", tt.Texts())
	}
	if !tt.HasText("文件夹：C:/music") {
		t.Errorf("a folders address is not shown: %q", tt.Texts())
	}
	// The page adds sources itself, every kind of them: a listener who
	// has a folder, or a script, adds it where its songs are seen.
	if !tt.HasText("添加音源") {
		t.Errorf("the page offers no way to add a source: %q", tt.Texts())
	}
}

// A source that lists nothing, and no other source that lists anything,
// leaves the page showing the sources: a list of no songs is not what a
// listener who keeps only a script came to see.
func TestSourcePageShowsSourcesWhenNoneList(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Sources = []source.Entry{pageScript("HYWmusic", "C:/music/HYWmusic.js", true)}
	a.sourceTests = map[string]string{}
	a.open(route{page: pageSource})
	a.sourceLanded(a.sourceGen, nil, nil)
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if !tt.HasText("HYWmusic") {
		t.Errorf("the page of a script alone shows %q", tt.Texts())
	}
	if tt.HasText("这里还没有歌曲") {
		t.Errorf("the page shows an empty list instead of the source: %q", tt.Texts())
	}
	// A source with nothing filled in yet says just that.
	a.settings.Sources = []source.Entry{pageScript("空白", "", true)}
	tt.Frame()
	if !tt.HasText("还没有填地址") {
		t.Errorf("a source that points nowhere says %q", tt.Texts())
	}
}

// The settings no longer pick a source file: that is what the page does,
// where the file is read. A folder is still picked there, since where it
// is is all a folder is.
// The settings no longer choose a source's place: a folder of this
// machine's music, and a source script, are added on the pages that
// list their songs. What the settings still do is point at the page
// of this machine's music, which is where a folder now lives.
func TestSettingsPointAtLocalMusic(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	folder := source.Entry{}
	folder.ID = "s-folder"
	folder.Kind = source.KindFolder
	folder.Value = "C:/music"
	folder.Enabled = true
	a.settings.Sources = []source.Entry{folder}
	tt := ui.NewTester(a.settingsView, 560, 1700)
	tt.Frame()
	// The one picker the settings have is for where songs are saved; a
	// source's folder and script are picked on their own pages, where the
	// songs are seen.
	if tt.HasText("选择音乐文件夹") || tt.HasText("导入") {
		t.Errorf("the settings still offer to pick a source: %q", tt.Texts())
	}
	if !tt.HasText("下载文件夹") {
		t.Errorf("the settings offer nowhere to save songs: %q", tt.Texts())
	}
	if !tt.HasText("本地音乐") || !tt.HasText("打开") {
		t.Errorf("the settings do not point at the page of local music: %q", tt.Texts())
	}
	// The folder is still edited here: its address is where it is.
	if !tt.HasText("本机文件夹") {
		t.Errorf("the settings no longer show the folder: %q", tt.Texts())
	}
}

// The add button on the page offers every kind of source by name, so that
// a folder of files on this machine is found without going through the
// settings for it: the menu is where the kinds are read.
func TestSourcePageAddMenu(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.open(route{page: pageSource})
	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	if err := tt.Click("添加音源"); err != nil {
		t.Fatalf("the page has no add button: %v", err)
	}
	tt.Frame()
	items := tt.Menu()
	joined := ""
	for _, it := range items {
		joined += it + " | "
	}
	for _, k := range source.Kinds {
		// The music of this machine has a page of its own and is
		// added there, so the menu is not where the folder is.
		if k.Kind == source.KindFolder {
			if strings.Contains(joined, k.Label) {
				t.Errorf("the menu still offers the folder: %q", joined)
			}
			continue
		}
		if !strings.Contains(joined, k.Label) {
			t.Errorf("the menu does not offer %q: %q", k.Label, joined)
		}
	}
}
