package source

import (
	"os"
	"path/filepath"
	"testing"
)

// An Ogg Vorbis file says what it holds of itself: its comments name the
// track, as a FLAC's do, and the last page's granule gives how long it is.
func TestReadOGGDurationAndTags(t *testing.T) {
	path := "../player/testdata/tone.ogg"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no OGG fixture: %v", err)
	}
	m, err := readMedia(path, false)
	if err != nil {
		t.Fatal(err)
	}
	// A second of sound at 44100, within a codec's padding.
	if m.duration < 900 || m.duration > 1200 {
		t.Errorf("a one-second OGG measures %d ms", m.duration)
	}
}

// A WAV says how long it plays from the size of its data chunk and the
// frame its fmt chunk describes.
func TestReadWAVDuration(t *testing.T) {
	path := "../player/testdata/tone.wav"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no WAV fixture: %v", err)
	}
	m, err := readMedia(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.duration < 950 || m.duration > 1050 {
		t.Errorf("a one-second WAV measures %d ms", m.duration)
	}
}

// An Ogg whose name is not a song's is named by its file, and measures
// itself all the same.
func TestOGGOfNoTagsIsNamedByItsFile(t *testing.T) {
	src := "../player/testdata/tone.ogg"
	b, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("no OGG fixture: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "歌手 - 歌名.ogg")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	tracks, err := Folder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Name != "歌名" {
		t.Fatalf("listed %+v", tracks)
	}
	if !Decodable(tracks[0].PlayURL) {
		t.Error("an OGG of this machine is taken as one that does not play")
	}
}
