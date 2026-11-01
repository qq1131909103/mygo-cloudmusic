package source

import (
	"os/exec"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/ffmpeg"
)

// A WMA is decoded by ffmpeg, and its length is read from what ffmpeg says of
// its header: a listing that shows 0:00 for it cannot show its progress.
func TestFFmpegDurationOfWMA(t *testing.T) {
	if !ffmpeg.Available() {
		t.Skip("no ffmpeg on this machine")
	}
	path := filepath.Join(t.TempDir(), "tone.wma")
	out, err := exec.Command(ffmpeg.Path(), "-v", "error", "-y", "-f", "lavfi", "-i",
		"sine=frequency=440:duration=3", "-c:a", "wmav2", path).CombinedOutput()
	if err != nil {
		t.Skipf("ffmpeg cannot make a WMA here: %v %s", err, out)
	}
	got := ffmpegDuration(path)
	if got < 2900 || got > 3200 {
		t.Fatalf("a 3 s WMA reads as %d ms", got)
	}
	m, err := readMedia(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.duration != got {
		t.Errorf("readMedia says %d ms, ffmpegDuration %d ms", m.duration, got)
	}
}
