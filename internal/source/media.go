package source

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// What a music file says of itself: its tags, how long it plays, and the
// cover inside it. Only the two formats the player streams are read, MP3
// and FLAC, and of each only what names a track, measures it and finds
// its cover: the music itself is the player's to decode.

// media is what a file says of itself.
type media struct {
	title    string
	artist   string
	album    string
	no       int    // the track's number on its album, 0 if unknown
	duration int64  // milliseconds; 0 if unknown
	art      []byte // the cover inside the file, when it is asked for
	hasArt   bool
}

// maxTagRead is the most of a file's tags read at once: a cover inside
// one is a few hundred kilobytes, and nothing else there is big.
const maxTagRead = 16 << 20

// readMedia reads what the file at path says of itself, with its cover if
// withArt, whose bytes come back in art.
func readMedia(path string, withArt bool) (media, error) {
	f, err := os.Open(path)
	if err != nil {
		return media{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return media{}, err
	}
	var magic [4]byte
	n, _ := io.ReadFull(f, magic[:])
	switch {
	case n == 4 && string(magic[:]) == "fLaC":
		return readFLAC(f, info.Size(), withArt)
	case n == 4 && string(magic[:]) == "OggS":
		return readOGG(f, info.Size(), withArt)
	case n == 4 && string(magic[:]) == "RIFF":
		return readWAV(f, info.Size())
	case n == 4 && string(magic[:3]) == "ID3":
		return readMP3(f, info.Size(), withArt)
	case n >= 2 && magic[0] == 0xff && magic[1]&0xe0 == 0xe0:
		// An MP3 frame's sync, which a file of another format being read
		// as one would not start with.
		return readMP3(f, info.Size(), withArt)
	case strings.EqualFold(filepath.Ext(path), ".mp3"):
		// An MP3 whose first bytes say nothing — one that only carries the
		// oldest tag, at its end: it is read as the name it was given.
		return readMP3(f, info.Size(), withArt)
	default:
		// A format the app does not read: it is listed by its file's name,
		// and left for a player that has its decoder. Its length is what
		// ffmpeg says of it, where there is one.
		return media{duration: ffmpegDuration(path)}, nil
	}
}

// readAtMost reads up to n bytes at off, or fewer at the file's end.
func readAtMost(f *os.File, off, n int64) []byte {
	if n <= 0 {
		return nil
	}
	b := make([]byte, min(n, maxTagRead))
	got, _ := f.ReadAt(b, off)
	return b[:got]
}

// tags fills in what the file did not say yet, from what its tags say.
func (m *media) tags(title, artist, album, no string) {
	if m.title == "" {
		m.title = strings.TrimSpace(title)
	}
	if m.artist == "" {
		m.artist = strings.TrimSpace(artist)
	}
	if m.album == "" {
		m.album = strings.TrimSpace(album)
	}
	if m.no == 0 {
		m.no = trackNumber(no)
	}
}

// trackNumber reads the number of a tag that names the track's place on
// its album, which some write as "3" and some as "3/12".
func trackNumber(s string) int {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "/")
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// FLAC. The STREAMINFO metadata block gives the sample rate and how many
// samples there are, which is how long the music is; the comment block
// gives the tags, and a picture block the cover.

const (
	blockStreamInfo = 0
	blockVorbis     = 4
	blockPicture    = 6
)

func readFLAC(f *os.File, size int64, withArt bool) (media, error) {
	var m media
	var hdr [4]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil || string(hdr[:]) != "fLaC" {
		return m, errors.New("source: not a FLAC file")
	}
	for pos := int64(4); pos+4 <= size; {
		if _, err := f.ReadAt(hdr[:], pos); err != nil {
			break
		}
		last := hdr[0]&0x80 != 0
		kind := hdr[0] & 0x7f
		n := int64(hdr[1])<<16 | int64(hdr[2])<<8 | int64(hdr[3])
		pos += 4
		if n < 0 || pos+n > size {
			break
		}
		switch kind {
		case blockStreamInfo:
			if b := readAtMost(f, pos, n); len(b) >= 18 {
				m.duration = flacLength(b)
			}
		case blockVorbis:
			if b := readAtMost(f, pos, n); len(b) > 0 {
				m.comments(vorbisComments(b))
			}
		case blockPicture:
			if b := readAtMost(f, pos, n); len(b) > 0 {
				m.hasArt = true
				if withArt {
					m.art = pictureData(b)
				}
			}
		}
		pos += n
		if last {
			break
		}
	}
	return m, nil
}

// flacLength returns how long a STREAMINFO block's music is, in
// milliseconds.
func flacLength(b []byte) int64 {
	rate := int64(b[10])<<12 | int64(b[11])<<4 | int64(b[12])>>4
	samples := int64(b[13]&0x0f)<<32 | int64(b[14])<<24 | int64(b[15])<<16 | int64(b[16])<<8 | int64(b[17])
	if rate == 0 {
		return 0
	}
	return samples * 1000 / rate
}

// vorbisComments reads a Vorbis comment block: the vendor, then one
// "KEY=value" for each comment, each of them little-endian length first.
func vorbisComments(b []byte) map[string][]string {
	if len(b) < 4 {
		return nil
	}
	pos := 4 + int(binary.LittleEndian.Uint32(b))
	if pos+4 > len(b) {
		return nil
	}
	count := int(binary.LittleEndian.Uint32(b[pos:]))
	pos += 4
	out := make(map[string][]string, count)
	for range min(count, 4096) {
		if pos+4 > len(b) {
			break
		}
		n := int(binary.LittleEndian.Uint32(b[pos:]))
		pos += 4
		if n < 0 || pos+n > len(b) {
			break
		}
		if key, value, ok := strings.Cut(string(b[pos:pos+n]), "="); ok {
			key = strings.ToUpper(strings.TrimSpace(key))
			out[key] = append(out[key], value)
		}
		pos += n
	}
	return out
}

// comments takes what a FLAC's comments say of the track.
func (m *media) comments(c map[string][]string) {
	first := func(key string) string {
		if v := c[key]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	m.tags(first("TITLE"), strings.Join(c["ARTIST"], ", "), first("ALBUM"), first("TRACKNUMBER"))
}

// pictureData reads the image out of a FLAC picture block: its MIME type
// and description first, then four numbers wide, then the image itself.
func pictureData(b []byte) []byte {
	pos := 4 // the picture's type
	for range 2 {
		if pos+4 > len(b) {
			return nil
		}
		n := int(binary.BigEndian.Uint32(b[pos:]))
		pos += 4
		if n < 0 || pos+n > len(b) {
			return nil
		}
		pos += n
	}
	pos += 16 // width, height, depth and how many colors
	if pos+4 > len(b) {
		return nil
	}
	n := int(binary.BigEndian.Uint32(b[pos:]))
	pos += 4
	if n <= 0 || pos+n > len(b) {
		return nil
	}
	return b[pos : pos+n]
}

// MP3. The ID3v2 tag at the start of the file gives the tags and the
// cover; the ID3v1 tag at its end, of the oldest files, gives tags alone.
// The length comes from the Xing or Info header of the first frame when it
// has one — which most files do — or else from the frame's bit rate and
// how many bytes are left of the file.

// OGG. The comment header carries the same Vorbis comments a FLAC's does,
// and the length is the last page's granule, a count of samples at the
// rate the identification header names.
func readOGG(f *os.File, size int64, withArt bool) (media, error) {
	var m media
	head := readAtMost(f, 0, min(size, 256<<10))
	var rate int64
	if i := bytes.Index(head, []byte("\x01vorbis")); i >= 0 && i+16 <= len(head) {
		rate = int64(binary.LittleEndian.Uint32(head[i+12:]))
	}
	if i := bytes.Index(head, []byte("\x03vorbis")); i >= 0 && i+7 <= len(head) {
		m.comments(vorbisComments(head[i+7:]))
	}
	// The last page says how many samples the file holds.
	tail := readAtMost(f, max(0, size-64<<10), 64<<10)
	if j := bytes.LastIndex(tail, []byte("OggS")); j >= 0 && j+14 <= len(tail) {
		granule := int64(binary.LittleEndian.Uint64(tail[j+6:]))
		if rate > 0 && granule > 0 {
			m.duration = granule * 1000 / rate
		}
	}
	return m, nil
}

// WAV. The fmt chunk says the rate and the frame size; the data chunk's
// size is how much sound there is, which the two turn into a length.
// Tags are ID3v2 in an "id3 " chunk, as some writers put them.
func readWAV(f *os.File, size int64) (media, error) {
	var m media
	head := readAtMost(f, 0, min(size, 1<<20))
	if len(head) < 12 || string(head[8:12]) != "WAVE" {
		return m, errors.New("source: not a WAV file")
	}
	var rate, channels, bits, dataSize int64
	for pos := int64(12); pos+8 <= int64(len(head)); {
		id := string(head[pos : pos+4])
		n := int64(binary.LittleEndian.Uint32(head[pos+4:]))
		body := pos + 8
		switch id {
		case "fmt ":
			if body+16 <= int64(len(head)) {
				channels = int64(binary.LittleEndian.Uint16(head[body+2:]))
				rate = int64(binary.LittleEndian.Uint32(head[body+4:]))
				bits = int64(binary.LittleEndian.Uint16(head[body+14:]))
			}
		case "data":
			dataSize = n
		}
		if n < 0 {
			break
		}
		pos = body + n + n%2
	}
	frame := channels * bits / 8
	if rate > 0 && frame > 0 && dataSize > 0 {
		m.duration = dataSize / frame * 1000 / rate
	}
	return m, nil
}

const id3v1Size = 128

func readMP3(f *os.File, size int64, withArt bool) (media, error) {
	var m media
	var hdr [10]byte
	n, _ := f.ReadAt(hdr[:], 0)
	audio := int64(0)
	if n == 10 && string(hdr[:3]) == "ID3" {
		tag := int64(synchsafe(hdr[6:10]))
		end := min(int64(len(hdr))+tag, size)
		// A tag that does not read leaves the file's name and its music:
		// its tags are not worth losing a track over.
		tagFields(f, hdr[3], hdr[5], int64(len(hdr)), end, &m, withArt)
		audio = end
	}
	m.duration = mp3Length(f, size, audio)
	if m.title == "" && m.artist == "" {
		id3v1Fields(f, size, &m)
	}
	return m, nil
}

// synchsafe reads a 28-bit length, seven bits a byte, as ID3v2 writes one.
func synchsafe(b []byte) int {
	n := 0
	for _, c := range b[:min(4, len(b))] {
		n = n<<7 | int(c&0x7f)
	}
	return n
}

// tagFields reads what an ID3v2 tag says, of the version and flags its
// header holds, from start to end.
func tagFields(f *os.File, version, flags byte, start, end int64, m *media, withArt bool) {
	// Unsynchronised frames are written with their 0xff bytes escaped,
	// which this reader does not undo: the length still reads.
	if version >= 4 && flags&0x80 != 0 {
		return
	}
	header := int64(10)
	if version == 2 {
		header = 6
	}
	if flags&0x40 != 0 {
		// An extended header, of its own length, before the frames.
		if b := readAtMost(f, start, 6); len(b) >= 6 {
			if version >= 4 {
				start += int64(synchsafe(b[:4])) + 4
			} else {
				start += 4 + int64(binary.BigEndian.Uint32(b))
			}
		}
	}
	for pos := start; pos+header <= end; {
		b := readAtMost(f, pos, header)
		if len(b) < int(header) {
			return
		}
		var id string
		var size int64
		switch version {
		case 2:
			id = string(b[:3])
			size = int64(b[3])<<16 | int64(b[4])<<8 | int64(b[5])
		case 4:
			id = string(b[:4])
			size = int64(synchsafe(b[4:8]))
		default:
			id = string(b[:4])
			size = int64(binary.BigEndian.Uint32(b[4:8]))
		}
		pos += header
		if size <= 0 || pos+size > end {
			return
		}
		switch {
		case id == "APIC" || id == "PIC":
			m.hasArt = true
			if withArt {
				if m.art == nil {
					m.art = apicData(readAtMost(f, pos, size))
				}
			}
		case size <= 1<<16:
			text := func(kind ...string) string {
				for _, k := range kind {
					if k == id {
						return decodeText(readAtMost(f, pos, size))
					}
				}
				return ""
			}
			m.tags(text("TIT2", "TT2"), text("TPE1", "TP1"), text("TALB", "TAL"), text("TRCK", "TRK"))
		}
		pos += size
	}
}

// apicData reads the image out of an attached picture frame: its MIME
// type, its kind and its description first, in one encoding or another,
// and the image after them. The image is found by what it starts with,
// which no description is written with.
func apicData(b []byte) []byte {
	for _, magic := range [][]byte{
		{0xff, 0xd8, 0xff},       // JPEG
		{0x89, 0x50, 0x4e, 0x47}, // PNG
		{'G', 'I', 'F', '8'},     // GIF
		{'R', 'I', 'F', 'F'},     // WEBP, in a RIFF
		{'B', 'M'},               // BMP
	} {
		if i := bytes.Index(b, magic); i >= 0 {
			return b[i:]
		}
	}
	return nil
}

// id3v1Fields reads the ID3v1 tag of the file's last 128 bytes.
func id3v1Fields(f *os.File, size int64, m *media) {
	if size < id3v1Size {
		return
	}
	b := readAtMost(f, size-id3v1Size, id3v1Size)
	if len(b) < id3v1Size || string(b[:3]) != "TAG" {
		return
	}
	field := func(from, to int) string {
		return strings.TrimRight(latin1Text(b[from:to]), "\x00 ")
	}
	m.tags(field(3, 33), field(33, 63), field(63, 93), "")
}

// mp3Length measures the MP3 whose audio starts at audio, in
// milliseconds.
func mp3Length(f *os.File, size, audio int64) int64 {
	// The first frame sits just past the tags; junk before it, if any, is
	// looked through, but not far.
	b := readAtMost(f, audio, 256<<10)
	at := firstFrame(b)
	if at < 0 {
		return 0
	}
	b = b[at:]
	fr, ok := parseMP3Frame(b)
	if !ok {
		return 0
	}
	if frames, ok := xingFrames(b, fr); ok {
		return frames * fr.samples * 1000 / int64(fr.rate)
	}
	if fr.bitrate == 0 || size <= audio+int64(at) {
		return 0
	}
	// A constant bit rate: the music is what is left of the file.
	return (size - audio - int64(at)) * 8 * 1000 / int64(fr.bitrate)
}

// firstFrame returns where an MPEG frame starts in b whose header the
// frame after it confirms, or -1. A file too short or too broken to hold
// two frames still starts with one: the first header that reads is it.
func firstFrame(b []byte) int {
	first := -1
	for i := 0; i+4 <= len(b); i++ {
		fr, ok := parseMP3Frame(b[i:])
		if !ok {
			continue
		}
		if first < 0 {
			first = i
		}
		next := i + int(fr.size)
		if next+4 > len(b) {
			return i
		}
		if _, ok := parseMP3Frame(b[next:]); ok {
			return i
		}
	}
	return first
}

// mp3Frame is what the header of an MPEG audio frame says.
type mp3Frame struct {
	size    int   // the frame's length in bytes
	rate    int   // samples a second
	samples int64 // what the frame sounds
	bitrate int   // bits a second, 0 if the header does not name one
	version int   // 1 for MPEG-1, 2 for MPEG-2, 25 for MPEG-2.5
	mono    bool
}

var (
	mp3BitratesV1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	mp3BitratesV2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	mp3Rates      = [4]int{44100, 48000, 32000, 0}
)

// parseMP3Frame reads the frame header b starts with, if it starts with
// one.
func parseMP3Frame(b []byte) (mp3Frame, bool) {
	var fr mp3Frame
	if len(b) < 4 || b[0] != 0xff || b[1]&0xe0 != 0xe0 {
		return fr, false
	}
	switch (b[1] >> 3) & 3 { // 3: MPEG-1, 2: MPEG-2, 0: MPEG-2.5
	case 3:
		fr.version = 1
	case 2:
		fr.version = 2
	case 0:
		fr.version = 25
	default:
		return fr, false
	}
	if (b[1]>>1)&3 != 1 { // layer III
		return fr, false
	}
	bi, ri := b[2]>>4, (b[2]>>2)&3
	if bi == 0 || bi == 15 || ri == 3 {
		return fr, false
	}
	rate := mp3Rates[ri]
	bitrate := mp3BitratesV1[bi]
	switch fr.version {
	case 2:
		rate, bitrate = rate/2, mp3BitratesV2[bi]
	case 25:
		rate, bitrate = rate/4, mp3BitratesV2[bi]
	}
	fr.rate, fr.bitrate = rate, bitrate*1000
	fr.samples = 1152
	// MPEG-2 and 2.5 sound half as many samples a frame.
	numerator := 144
	if fr.version != 1 {
		fr.samples, numerator = 576, 72
	}
	fr.size = numerator*fr.bitrate/fr.rate + int(b[2]>>1)&1
	fr.mono = b[3]>>6 == 3
	return fr, fr.size > 0
}

// xingFrames reads how many frames a file's first frame says it holds,
// from the Xing or Info header a variable bit rate file writes there.
func xingFrames(b []byte, fr mp3Frame) (int64, bool) {
	side := 32
	if fr.mono {
		side = 17
	}
	if fr.version != 1 {
		side = 9
		if !fr.mono {
			side = 17
		}
	}
	at := 4 + side
	if at+8 > len(b) {
		return 0, false
	}
	if tag := string(b[at : at+4]); tag != "Xing" && tag != "Info" {
		return 0, false
	}
	if binary.BigEndian.Uint32(b[at+4:])&1 == 0 {
		return 0, false
	}
	frames := int64(binary.BigEndian.Uint32(b[at+8:]))
	return frames, frames > 0
}

// decodeText reads a tag's text, which says in its first byte how it is
// written: Latin-1, UTF-16 with a byte order mark, UTF-16 the big-end
// way, or UTF-8.
func decodeText(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	text := ""
	switch b[0] {
	case 0:
		text = latin1Text(b[1:])
	case 1, 2:
		text = utf16Text(b[1:], b[0] == 1)
	default:
		text = string(b[1:])
	}
	return strings.TrimSpace(strings.Trim(text, "\x00"))
}

// latin1Text reads text a tag says is ISO-8859-1, the oldest encoding,
// which tools writing Chinese write as UTF-8 anyway: text that reads as
// UTF-8 is taken for it.
func latin1Text(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	return latin1(b)
}

// latin1 reads bytes as ISO-8859-1, the encoding the oldest tags use.
func latin1(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

// utf16Text reads UTF-16 text, little-endian when mark says it starts with
// a byte order mark and the mark says so, or else the way the tag says.
func utf16Text(b []byte, mark bool) string {
	big := false
	if mark {
		switch {
		case len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe:
			b = b[2:]
		case len(b) >= 2 && b[0] == 0xfe && b[1] == 0xff:
			big, b = true, b[2:]
		default:
			// No mark: the little way, as most writers write it.
		}
	}
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if big {
			units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			units = append(units, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	s := string(utf16.Decode(units))
	return strings.ReplaceAll(s, "\x00", "")
}
