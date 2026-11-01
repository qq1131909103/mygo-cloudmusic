package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/ffmpeg"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// forceNoFFmpeg makes the app see no ffmpeg for the length of the test, so
// the formats only ffmpeg decodes are the ones it does not play, whatever
// this machine has installed.
func forceNoFFmpeg(t *testing.T) {
	before := ffmpeg.Path()
	ffmpeg.SetPath("")
	t.Cleanup(func() { ffmpeg.SetPath(before) })
}

// A file of this machine in a format the app does not decode is listed,
// marked as one that does not play, and its length reads as unknown rather
// than as nothing: it was scanned to be seen.
func TestUnsupportedFormatIsListedAndMarked(t *testing.T) {
	forceNoFFmpeg(t)
	if ffmpeg.Available() {
		t.Skip("this machine has ffmpeg, so the formats it decodes play")
	}
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Sources = []source.Entry{
		{ID: "l", Name: "我的音乐", Kind: source.KindFolder, Value: "D:/音乐", Enabled: true},
	}
	a.open(route{page: pageLocal})
	listed := func(id int64, name, path string, ms int64) netease.Track {
		return netease.Track{ID: id, Name: name, PlayURL: path, Source: "我的音乐", Duration: ms}
	}
	a.localSongs = []netease.Track{
		listed(1, "能播的歌", "D:/音乐/能播的歌.flac", 200000),
		listed(2, "m4a的歌", "D:/音乐/m4a的歌.m4a", 0),
	}

	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	tt.Advance(0)
	tt.Frame()

	if !tt.HasText("m4a的歌") {
		t.Fatalf("the file of a format it does not decode is not listed: %v", tt.Texts())
	}
	if !tt.HasText("不支持播放") {
		t.Errorf("it is not marked as one that does not play: %v", tt.Texts())
	}
	if !tt.HasText("—") {
		t.Errorf("its length reads as something other than unknown: %v", tt.Texts())
	}
	// The one it decodes itself says neither.
	if !a.canPlay(&netease.Track{PlayURL: "D:/音乐/能播的歌.flac"}) {
		t.Error("a FLAC of this machine is taken as one that does not play")
	}
	if a.canPlay(&netease.Track{PlayURL: "D:/音乐/m4a的歌.m4a"}) {
		t.Error("an M4A is taken as one that plays with no ffmpeg on the machine")
	}
}

// Which formats play is decided by whether there is an ffmpeg for the ones
// the app has no decoder of its own for: the rest play either way.
func TestDecidableDependsOnFFmpeg(t *testing.T) {
	if !source.Decodable("D:/音乐/歌.flac") || !source.Decodable("D:/音乐/歌.mp3") {
		t.Error("the formats the app decodes itself need no ffmpeg")
	}
	if got, want := source.Decodable("D:/音乐/歌.m4a"), ffmpeg.Available(); got != want {
		t.Errorf("an M4A decidable %v, want %v (ffmpeg there: %v)", got, want, ffmpeg.Available())
	}
}

// Pressing a row that cannot play says why, rather than doing nothing.
func TestUnsupportedRowSaysWhy(t *testing.T) {
	forceNoFFmpeg(t)
	if ffmpeg.Available() {
		t.Skip("this machine has ffmpeg, so the format plays")
	}
	tr := netease.Track{ID: 2, Name: "m4a的歌", PlayURL: "D:/音乐/m4a的歌.m4a"}
	note, ok := notPlayableNote(&tr)
	if !ok {
		t.Fatal("a row of a format it does not decode says nothing")
	}
	if !contains(note, "M4A") {
		t.Errorf("the note is %q, which does not name the format", note)
	}
	// A song of NetEase's that its privilege withholds is not this case.
	withheld := netease.Track{ID: 5, Name: "会员歌"}
	if _, ok := notPlayableNote(&withheld); ok {
		t.Error("a song of NetEase's was taken for a file of this machine")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// A length under a second is no length a song has: a file whose length the
// reader did not find shows a dash, not 0:00.
func TestClockOrDashUnderASecond(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{{0, "—"}, {400 * time.Millisecond, "—"}, {time.Second, "0:01"}, {214 * time.Second, "3:34"}} {
		if got := clockOrDash(c.d); got != c.want {
			t.Errorf("clockOrDash(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
