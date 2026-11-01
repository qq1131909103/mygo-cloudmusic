package player

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"netease-music-mygo/internal/ffmpeg"
)

// An external stream plays a file the app has no decoder for by having
// ffmpeg decode it: a WMA in an ASF container, an M4A of AAC in an MP4
// one. ffmpeg is asked for the song as raw PCM, which is read as it
// arrives, so playback starts with the first samples and no temporary
// file is left behind.
type externalStream struct {
	path       string
	durationMs int64

	mu     sync.Mutex
	cmd    *exec.Cmd
	out    io.ReadCloser
	cancel context.CancelFunc
	rate   int
	ch     int
	pos    int64 // frames decoded from the file's start
	total  int64 // frames the file is said to hold, 0 when unknown
	buf    []byte
	err    string
	spec   *spectrum
	dl     *download // for the stream interface: the file is at hand, whole
	once   sync.Once
}

// externalRate is what ffmpeg is asked to decode to: the rate the app's own
// streams mostly run at, which the mixer resamples from anyway.
const externalRate = 44100

// externalChannels is how many channels ffmpeg is asked for: stereo, which
// is what the player mixes.
const externalChannels = 2

// openExternal starts ffmpeg decoding the file at path. durationMs is what
// the file is said to be, for the length a listener seeks against.
func openExternal(path string, durationMs int64) (*externalStream, error) {
	if ffmpeg.Path() == "" {
		return nil, errors.New("这个格式要用 ffmpeg 播放，但这台机器上没找到")
	}
	s := &externalStream{
		path:       path,
		durationMs: durationMs,
		rate:       externalRate,
		ch:         externalChannels,
		dl:         downloaded(nil),
	}
	if durationMs > 0 {
		s.total = durationMs * externalRate / 1000
	}
	s.spec = newSpectrum(s.rate, s.total)
	if err := s.start(0); err != nil {
		return nil, err
	}
	return s, nil
}

// start runs ffmpeg reading the file, from the frame at, which it is told
// to begin at: the container's index knows where the sound at that time is.
func (s *externalStream) start(at int64) error {
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{"-v", "error", "-nostdin"}
	if at > 0 {
		args = append(args, "-ss", strconv.FormatFloat(float64(at)/float64(s.rate), 'f', 3, 64))
	}
	args = append(args,
		"-i", s.path,
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ac", strconv.Itoa(externalChannels),
		"-ar", strconv.Itoa(externalRate),
		"-",
	)
	cmd := exec.CommandContext(ctx, ffmpeg.Path(), args...)
	ffmpeg.HideWindow(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return errors.New("ffmpeg 启动不了：" + err.Error())
	}
	s.mu.Lock()
	s.cmd, s.out, s.cancel, s.pos, s.err = cmd, out, cancel, at, ""
	s.mu.Unlock()
	go s.drainErrors(errPipe)
	return nil
}

// drainErrors keeps ffmpeg's own diagnostics read, so that a decoder which
// fails does not block on a full pipe, and keeps the last line, for a
// failure to quote.
func (s *externalStream) drainErrors(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		s.mu.Lock()
		s.err = line
		s.mu.Unlock()
	}
}

func (s *externalStream) SampleRate() int { return s.rate }
func (s *externalStream) Channels() int   { return s.ch }

// Len returns the frames the file holds, or -1 when it is not known.
func (s *externalStream) Len() int64 {
	if s.total > 0 {
		return s.total
	}
	return -1
}

// Read decodes the next samples as interleaved stereo in [-1, 1], which is
// what the player mixes, waiting for ffmpeg to write them.
func (s *externalStream) Read(dst []float32) (int, error) {
	frames := len(dst) / s.ch
	if frames == 0 {
		return 0, nil
	}
	need := frames * s.ch * 2 // signed 16-bit
	if cap(s.buf) < need {
		s.buf = make([]byte, need)
	}
	b := s.buf[:need]
	s.mu.Lock()
	out := s.out
	s.mu.Unlock()
	if out == nil {
		return 0, io.EOF
	}
	got, err := io.ReadFull(out, b)
	got -= got % (s.ch * 2)
	if got == 0 {
		if err == nil || err == io.ErrUnexpectedEOF {
			err = io.EOF
		}
		return 0, err
	}
	n := got / 2
	for i := range n {
		v := int16(uint16(b[2*i]) | uint16(b[2*i+1])<<8)
		dst[i] = float32(v) / (1 << 15)
	}
	s.mu.Lock()
	at := s.pos
	s.pos += int64(n / s.ch)
	s.mu.Unlock()
	s.spec.feed(at, dst[:n])
	if err == io.ErrUnexpectedEOF {
		err = nil
	}
	return n, err
}

// SetPosition goes to frame by starting ffmpeg again, reading the file from
// there: the container's index is what knows where the sound is.
func (s *externalStream) SetPosition(frame int64) error {
	if frame < 0 {
		frame = 0
	}
	if s.total > 0 && frame > s.total {
		frame = s.total
	}
	s.stop()
	if err := s.start(frame); err != nil {
		return err
	}
	// Levels reads the spectrum from the playing goroutine, so the swap is
	// made under the lock it reads it by.
	s.mu.Lock()
	s.spec = newSpectrum(s.rate, s.total)
	s.mu.Unlock()
	return nil
}

// stop ends the ffmpeg reading the file, waiting for it to go: it is told
// to stop, and killed if it does not.
func (s *externalStream) stop() {
	s.mu.Lock()
	cmd, out, cancel := s.cmd, s.out, s.cancel
	s.cmd, s.out, s.cancel = nil, nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if out != nil {
		out.Close()
	}
	if cmd != nil && cmd.Process != nil {
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			cmd.Process.Kill()
		}
	}
}

func (s *externalStream) source() (*download, *spectrum) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dl, s.spec
}

func (s *externalStream) Close() error {
	s.once.Do(s.stop)
	return nil
}

// Err returns why ffmpeg last failed, if it did.
func (s *externalStream) Err() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
