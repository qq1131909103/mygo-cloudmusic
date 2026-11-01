package source

import (
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"netease-music-mygo/internal/ffmpeg"
)

// durationRE finds the length ffmpeg reports for a file it is given to read:
// "Duration: 00:00:05.01,".
var durationRE = regexp.MustCompile(`Duration: (\d+):(\d{2}):(\d{2})\.(\d+)`)

// ffmpegDuration says how long a file of a format ffmpeg decodes is, in
// milliseconds, or 0 when it cannot say. Only the file's header is read, so
// nothing is decoded and it is quick enough to ask on every listing.
func ffmpegDuration(path string) int64 {
	if !ffmpeg.Handled(path) || !ffmpeg.Available() {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg.Path(), "-hide_banner", "-nostdin", "-i", path)
	ffmpeg.HideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// ffmpeg exits with an error when given no output; the length is in what
	// it says before that, so the exit status is not what is read.
	_ = cmd.Run()
	m := durationRE.FindStringSubmatch(stderr.String())
	if m == nil {
		return 0
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	s, _ := strconv.Atoi(m[3])
	frac := m[4]
	ms, _ := strconv.Atoi((frac + "000")[:3])
	return int64(h)*3600_000 + int64(mi)*60_000 + int64(s)*1000 + int64(ms)
}
