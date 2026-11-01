package player

import (
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/ffmpeg"
	"netease-music-mygo/internal/netease"
)

// The formats the app has no decoder of its own for play through ffmpeg,
// where there is one: a WMA in an ASF container and an M4A of AAC in an
// MP4 one. Without ffmpeg the app says so, rather than playing noise.
func TestFormatsFFmpegDecodes(t *testing.T) {
	if !ffmpeg.Available() {
		t.Skip("no ffmpeg on this machine")
	}
	for _, c := range []struct {
		file string
		typ  string
	}{
		{"testdata/tone.wma", "wma"},
		{"testdata/tone.m4a", "m4a"},
	} {
		path, err := filepath.Abs(c.file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Skipf("no fixture %s: %v", c.file, err)
		}
		var tr netease.Track
		tr.ID, tr.Name, tr.Duration, tr.PlayURL = -1, "tone", 1000, path
		st, dl, err := ownSource(tr, "", "")
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if st.Type != c.typ {
			t.Errorf("%s names its type %q, want %q", c.file, st.Type, c.typ)
		}
		s, err := openStream(dl, tr.Duration)
		if err != nil {
			dl.Close()
			t.Fatalf("%s: open: %v", c.file, err)
		}
		if _, ok := s.(*externalStream); !ok {
			t.Errorf("%s is not decoded through ffmpeg: %T", c.file, s)
		}
		got := playAll(t, s)
		frames := len(got) / 2
		var peak float32
		for _, v := range got {
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		if frames == 0 || peak < 0.05 {
			t.Fatalf("%s decoded %d frames, peak %v", c.file, frames, peak)
		}
		if rate := s.SampleRate(); rate != 44100 {
			t.Errorf("%s decoded at %d Hz, want 44100", c.file, rate)
		}
		if frames < 44100*8/10 || frames > 44100*12/10 {
			t.Errorf("%s decoded %d frames, want about a second", c.file, frames)
		}
		s.Close()
	}
}

// Seeking restarts the decoder at the time asked for, so that dragging the
// bar lands where it was dragged.
func TestExternalSeek(t *testing.T) {
	if !ffmpeg.Available() {
		t.Skip("no ffmpeg on this machine")
	}
	path, err := filepath.Abs("testdata/tone.m4a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no fixture: %v", err)
	}
	var tr netease.Track
	tr.ID, tr.Name, tr.Duration, tr.PlayURL = -1, "tone", 3000, path
	_, dl, err := ownSource(tr, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()
	s, err := openStream(dl, tr.Duration)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// From a time past the end of the second of sound: nothing is left.
	if err := s.SetPosition(2 * 44100); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if got := playAll(t, s); len(got)/2 > 44100/4 {
		t.Errorf("seeking past the end decoded %d frames", len(got)/2)
	}
}

// A format the app cannot decode, on a machine with no ffmpeg, is said to
// need one rather than played as noise.
func TestNoFFmpegSaysSo(t *testing.T) {
	if ffmpeg.Available() {
		t.Skip("this machine has ffmpeg")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "song.wma")
	if err := os.WriteFile(path, []byte("0&\xb2u\x8ef\xcf\x11\xa6\xd9\x00\xaa\x00b\xcelf"), 0o600); err != nil {
		t.Fatal(err)
	}
	var tr netease.Track
	tr.ID, tr.Name, tr.Duration, tr.PlayURL = -1, "song", 1000, path
	_, dl, err := ownSource(tr, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()
	if _, err := openStream(dl, tr.Duration); err == nil {
		t.Error("a WMA played with no ffmpeg on the machine")
	}
}
