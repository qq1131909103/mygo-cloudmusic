package ffmpeg

import (
	"os"
	"path/filepath"
	"testing"
)

// Handled names the formats ffmpeg is asked for: the ones a listener's own
// library is in which the app has no decoder of its own for.
func TestHandled(t *testing.T) {
	for _, c := range []struct {
		path string
		want bool
	}{
		{"a.wma", true},
		{"a.WMA", true},
		{"a.m4a", true},
		{"a.aac", true},
		{"a.opus", true},
		{"a.flac", false},
		{"a.mp3", false},
		{"a.ogg", false},
		{"a.wav", false},
		{"a.txt", false},
	} {
		if got := Handled(c.path); got != c.want {
			t.Errorf("%s handled %v, want %v", c.path, got, c.want)
		}
	}
}

// A copy beside the app is what is used, before one on the PATH: a build
// that ships ffmpeg means that one to be asked.
func TestBundledCopyIsUsed(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ffmpeg.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := found
	SetPath(exe)
	t.Cleanup(func() { SetPath(before) })
	if Path() != exe {
		t.Errorf("Path is %q, want the copy beside the app %q", Path(), exe)
	}
	if !Available() {
		t.Error("a copy beside the app is not taken as one")
	}
}
