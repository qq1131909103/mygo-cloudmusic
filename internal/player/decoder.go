package player

import (
	"cmp"
	"errors"
	"io"
	"math"
	"sync"

	"github.com/egoist/mygo/audio"
)

// A decoderStream plays a download with one of the decoders of package
// audio: the OGG Vorbis and WAV files a folder of this machine holds, which
// the player's own MP3 and FLAC streams do not read. The decoder seeks by
// byte, as the format says where its frames are.
type decoderStream struct {
	dl   *download
	dec  audio.Decoder
	rate int
	ch   int
	pos  int64
	spec *spectrum
	drop func() // lets the download go, as the stream closes
	once sync.Once
	buf  []float32
}

// openDecoderStream opens dl with the decoder of package audio that reads
// its format: "OggS" for a Vorbis file, "RIFF????WAVE" for a WAV.
func openDecoderStream(dl *download) (*decoderStream, error) {
	dec, name, err := audio.NewDecoder(&downloadSeeker{d: dl})
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, errors.New("player: no decoder for this file")
	}
	s := &decoderStream{dl: dl, dec: dec, rate: dec.SampleRate(), ch: dec.Channels(), drop: dl.Close}
	if s.rate <= 0 || s.ch <= 0 {
		return nil, errors.New("player: the file says no rate or no channels")
	}
	s.spec = newSpectrum(s.rate, dec.Len())
	return s, nil
}

func (s *decoderStream) SampleRate() int { return s.rate }
func (s *decoderStream) Channels() int   { return 2 }
func (s *decoderStream) Len() int64      { return s.dec.Len() }

// Read decodes into dst as interleaved stereo, which is what the player
// mixes: a file of one channel has it on both.
func (s *decoderStream) Read(dst []float32) (int, error) {
	frames := len(dst) / 2
	if frames == 0 {
		return 0, nil
	}
	if cap(s.buf) < frames*s.ch {
		s.buf = make([]float32, frames*s.ch)
	}
	src := s.buf[:frames*s.ch]
	n, err := s.dec.Read(src)
	frames = n / s.ch
	if frames == 0 {
		if err == nil {
			err = io.EOF
		}
		return 0, err
	}
	if s.ch == 1 {
		for i := frames - 1; i >= 0; i-- {
			dst[2*i], dst[2*i+1] = src[i], src[i]
		}
	} else {
		for i := range frames {
			dst[2*i], dst[2*i+1] = src[s.ch*i], src[s.ch*i+1]
		}
	}
	s.spec.feed(s.pos, dst[:frames*2])
	s.pos += int64(frames)
	return frames * 2, err
}

func (s *decoderStream) SetPosition(frame int64) error {
	if err := s.dec.SetPosition(frame); err != nil {
		return err
	}
	s.pos = frame
	return nil
}

func (s *decoderStream) source() (*download, *spectrum) { return s.dl, s.spec }

func (s *decoderStream) Close() error {
	s.once.Do(s.drop)
	return nil
}

// A downloadSeeker reads a download as an io.ReadSeeker, which the
// decoders of package audio need: reading waits for the bytes to come in,
// and seeking to the end waits for the whole song, as a stream's length is
// what its last byte says.
type downloadSeeker struct {
	d   *download
	pos int64
}

func (r *downloadSeeker) Read(p []byte) (int, error) {
	d := r.d
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.n <= r.pos && !d.done {
		d.cond.Wait()
	}
	if d.n <= r.pos {
		if d.err != nil {
			return 0, d.err
		}
		return 0, io.EOF
	}
	n, err := d.readAt(p, r.pos)
	if n == 0 {
		return 0, cmp.Or(err, io.ErrUnexpectedEOF)
	}
	r.pos += int64(n)
	return n, nil
}

func (r *downloadSeeker) Seek(off int64, whence int) (int64, error) {
	d := r.d
	var from int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		from = r.pos
	case io.SeekEnd:
		if d.size <= 0 {
			d.waitFor(math.MaxInt64)
		}
		from = d.size
		if from <= 0 {
			from = d.waitFor(math.MaxInt64)
		}
	default:
		return 0, errors.New("player: seek from nowhere")
	}
	pos := from + off
	if pos < 0 {
		return 0, errors.New("player: seek before the start")
	}
	r.pos = pos
	return pos, nil
}
