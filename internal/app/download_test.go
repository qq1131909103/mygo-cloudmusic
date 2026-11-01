package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// A song saved from a source lands in the Downloads folder under the name
// it is known by, whole, and the button says so.
func TestDownloadSavesFromSource(t *testing.T) {
	body := []byte("ID3 fake mp3 bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write(body)
	}))
	defer srv.Close()

	a := newTestApp()
	a.settings = defaultSettings()
	a.downloads = &downloadState{dir: t.TempDir()}
	keepSources(a, source.Entry{
		ID: "s", Name: "我的音源", Kind: source.KindScript, Value: "/music/source.js", Enabled: true,
	})
	// The script is not run here: what it would have answered is what the
	// download is given, through the fallback the app keeps.
	a.fallbackSources.keep(nil)
	a.downloads.mu.Lock()
	a.downloads.current = nil
	a.downloads.mu.Unlock()

	tr := netease.Track{ID: 347230, Name: "海阔天空", Artists: []netease.Artist{{Name: "Beyond"}}, Duration: 200000}
	job := &songDownload{Track: tr, Level: "exhigh", URL: srv.URL + "/song.mp3"}
	a.downloads.mu.Lock()
	a.downloads.current = job
	a.downloads.mu.Unlock()

	path, err := uniquePath(filepath.Join(a.downloads.dir, fileName(tr, job.URL)))
	if err != nil {
		t.Fatal(err)
	}
	if err := fetchTo(job.URL, path, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("saved %d bytes, want the %d the source served", len(got), len(body))
	}
	if want := "Beyond - 海阔天空.mp3"; filepath.Base(path) != want {
		t.Errorf("saved as %q, want %q", filepath.Base(path), want)
	}
}

// A song is saved under a name a filesystem takes, and a second download
// of the same song is numbered rather than replacing the first.
func TestDownloadNamesAndNumbers(t *testing.T) {
	tr := netease.Track{Name: "a/b:c*d?e\"f<g>h|i", Artists: []netease.Artist{{Name: "X"}}}
	if got := fileName(tr, "https://cdn.test/a.mp3"); got != "X - a_b_c_d_e_f_g_h_i.mp3" {
		t.Errorf("saved as %q", got)
	}
	if got := fileName(tr, "https://cdn.test/a.flac"); !strings.HasSuffix(got, ".flac") {
		t.Errorf("a FLAC saved as %q", got)
	}

	dir := t.TempDir()
	first := filepath.Join(dir, "歌.mp3")
	if err := os.WriteFile(first, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := uniquePath(first)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "歌 (2).mp3"); second != want {
		t.Errorf("the second download goes to %q, want %q", second, want)
	}
}

// The badge under the progress bar says what plays in one: the quality
// the song streams at, and which source found it, or that it is NetEase's
// own. Both are one thing to say, so they are one badge.
func TestQualityBadge(t *testing.T) {
	tr := netease.Track{ID: 1, Name: "海阔天空"}
	for _, c := range []struct {
		stream *netease.Stream
		track  netease.Track
		want   string
	}{
		{&netease.Stream{Level: "exhigh", Source: "我的音源", URL: "https://cdn.test/a.mp3"}, tr, "极高（320k） · 我的音源"},
		{&netease.Stream{Level: "lossless", Source: "HYW", URL: "https://cdn.test/a.flac"}, tr, "无损（FLAC） · HYW"},
		{&netease.Stream{Level: "hires", Source: "HYW", URL: "https://cdn.test/a.flac"}, tr, "Hi-Res · HYW"},
		{&netease.Stream{Level: "hires", URL: "https://cdn.test/a.flac"}, tr, "Hi-Res"},
		{&netease.Stream{Level: "standard"}, tr, "标准（128k）"},
		{nil, tr, ""},
	} {
		label, _ := qualityOf(c.stream, &c.track)
		if label != c.want {
			t.Errorf("with %+v the badge says %q, want %q", c.stream, label, c.want)
		}
	}

	// A track the listener added itself, which names its own file.
	own := netease.Track{ID: -1, Name: "春日影", PlayURL: "D:/音乐/春日影.mp3"}
	label, _ := qualityOf(&netease.Stream{Level: "flac", URL: own.PlayURL}, &own)
	if label != "flac · 本机" {
		t.Errorf("a file of the listener's own says %q, want flac · 本机", label)
	}
}

// The button beside the song offers the best a source can give, and each
// quality the settings name, so that a listener can ask for one.
func TestDownloadButtonOffersTheQualities(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.downloads = &downloadState{dir: t.TempDir()}
	a.pl.Cue([]netease.Track{{ID: 1, Name: "一", Duration: 200000}})
	a.pl.SetLyrics(nil)
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.Click("下载"); err != nil {
		t.Fatal(err)
	}
	menu := tt.Menu()
	if len(menu) == 0 {
		t.Fatal("the download button opens no menu")
	}
	if menu[0] != "下载最高音质" {
		t.Errorf("the menu starts with %q, want 下载最高音质", menu[0])
	}
	found := false
	for _, item := range menu {
		if item == "下载极高（320k）" {
			found = true
		}
	}
	if !found {
		t.Errorf("the menu offers no quality to pick: %q", menu)
	}
}

// A song is saved where the settings say, and in the Downloads folder when
// they say nothing: one place for every song, not a question each time.
func TestDownloadDirFollowsSettings(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	picked := t.TempDir()
	a.settings.DownloadDir = picked
	dir, err := a.downloadDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != picked {
		t.Errorf("saved in %q, want the folder picked, %q", dir, picked)
	}
	if got := a.downloadDirPath(); got != picked {
		t.Errorf("the settings show %q, want %q", got, picked)
	}

	// With nothing picked, it is the Downloads folder, and the settings say
	// that it is the default.
	a.settings.DownloadDir = ""
	dir, err = a.downloadDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" || dir == picked {
		t.Errorf("saved in %q, want the Downloads folder", dir)
	}
	if got := a.downloadDirPath(); !strings.HasSuffix(got, "（默认）") {
		t.Errorf("the settings show %q, which does not say it is the default", got)
	}
}
