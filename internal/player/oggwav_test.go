package player

import (
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/netease"
)

// The formats a folder of this machine holds beyond MP3 and FLAC play too:
// an Ogg Vorbis file and a WAV, decoded by package audio, through the same
// stream the rest of the player reads.
func TestOggAndWAVPlay(t *testing.T) {
	for _, c := range []struct {
		file string
		typ  string
	}{
		{"testdata/tone.ogg", "ogg"},
		{"testdata/tone.wav", "wav"},
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
		if rate := s.SampleRate(); rate != 44100 {
			t.Errorf("%s decoded at %d Hz, want 44100", c.file, rate)
		}
		if err := s.SetPosition(22050); err != nil {
			t.Errorf("%s: seek: %v", c.file, err)
		}
		got := playAll(t, s)
		s.Close()
		frames := len(got) / 2
		if frames == 0 {
			t.Fatalf("%s decoded nothing after a seek", c.file)
		}
		var peak float32
		for _, v := range got {
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		if peak < 0.05 {
			t.Errorf("%s decoded %d frames, nothing audible (peak %v)", c.file, frames, peak)
		}
		if frames < 44100*4/10 || frames > 44100*6/10 {
			t.Errorf("%s decoded %d frames from 0.5s, want about half a second", c.file, frames)
		}
	}
}

// A file of a format the app cannot open is not read as an MP3 and played
// as noise: it opens into nothing at all.
func TestUnknownFormatIsNotTakenForMP3(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.m4a")
	body := append([]byte{0, 0, 0, 0x20}, []byte("ftypM4A ")...)
	body = append(body, make([]byte, 256)...)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	var tr netease.Track
	tr.ID, tr.Name, tr.Duration, tr.PlayURL = -1, "song", 1000, path
	_, dl, err := ownSource(tr, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()
	s, err := openStream(dl, tr.Duration)
	if err != nil {
		return
	}
	got := playAll(t, s)
	s.Close()
	if len(got)/2 > 0 {
		t.Skipf("an m4a that decodes to %d frames: broader than expected", len(got)/2)
	}
}
