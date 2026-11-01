package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// The words of a live take are timed to the record, and run ahead of what
// is heard: two arrows at the right of the window move them, one above the
// other, carrying no words of their own.
func TestLyricsShiftArrows(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	var tr netease.Track
	tr.ID, tr.Name, tr.Duration = 7, "歌", 200000
	a.pl.Cue([]netease.Track{tr})
	a.pl.SetLyrics([]lyrics.Line{{At: 0, Text: "第一句"}, {At: 10000, Text: "第二句"}})
	a.nowPlaying = true

	tt := ui.NewTester(a.view, 1200, 900)
	tt.Frame()
	tt.Advance(0)
	tt.Frame()

	// The arrows are found by what they say they do, not by a label drawn
	// beside them; the only thing written between them is the seconds.
	if err := tt.Click("歌词往后（晚一点）"); err != nil {
		t.Fatalf("the arrows are not there: %v", err)
	}
	tt.Frame()
	if a.settings.LyricsShiftMs != 100 {
		t.Errorf("the adjustment is %d ms, want 100", a.settings.LyricsShiftMs)
	}
	// How far they were moved is shown, and it is the seconds alone.
	if !tt.HasText("+0.1") {
		t.Errorf("the seconds moved are not shown: %v", tt.Texts())
	}
	got, _ := a.pl.Lyrics()
	if len(got) == 0 || got[0].At != 100*time.Millisecond {
		t.Errorf("the words did not move: %v", got)
	}
	// The other arrow moves them back, and the two cancel.
	if err := tt.Click("歌词往前（早一点）"); err != nil {
		t.Fatalf("click back: %v", err)
	}
	tt.Frame()
	if a.settings.LyricsShiftMs != 0 {
		t.Errorf("the adjustment is %d ms after stepping back", a.settings.LyricsShiftMs)
	}
	// Back at zero, the count reads zero, and no label of any kind is
	// drawn with it.
	if !tt.HasText("0") {
		t.Errorf("the count is not at zero: %v", tt.Texts())
	}
	for _, s := range tt.Texts() {
		if s == "歌词同步" {
			t.Errorf("a label is drawn beside the arrows: %q", s)
		}
	}
}

// Shift moves the lines and their words by d, without touching what it
// was given, and a shift of nothing costs no copy.
func TestLyricsShiftMoves(t *testing.T) {
	in := []lyrics.Line{{At: time.Second, End: 2 * time.Second, Text: "a"}}
	out := lyrics.Shift(in, 500*time.Millisecond)
	if out[0].At != 1500*time.Millisecond || out[0].End != 2500*time.Millisecond {
		t.Errorf("shifted to %v..%v", out[0].At, out[0].End)
	}
	if in[0].At != time.Second {
		t.Error("the lines it was given were changed")
	}
	if got := lyrics.Shift(in, 0); &got[0] != &in[0] {
		t.Error("a shift of nothing copied the lines")
	}
	withWords := []lyrics.Line{{At: time.Second, Words: []lyrics.Word{{Text: "a", Start: time.Second, End: 2 * time.Second}}}}
	moved := lyrics.Shift(withWords, time.Second)
	if moved[0].Words[0].Start != 2*time.Second || moved[0].Words[0].End != 3*time.Second {
		t.Errorf("the word moved to %v..%v", moved[0].Words[0].Start, moved[0].Words[0].End)
	}
	if withWords[0].Words[0].Start != time.Second {
		t.Error("the words it was given were changed")
	}
}
