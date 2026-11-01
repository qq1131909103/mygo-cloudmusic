package source

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"netease-music-mygo/internal/netease"
)

// artistOf is the first artist credited by a track, or "".
func artistOf(t netease.Track) string {
	if len(t.Artists) == 0 {
		return ""
	}
	return t.Artists[0].Name
}

// The tags of a file that has none are read as unknown, not as an error.
func TestReadMediaUnknownFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("this is not music"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := readMedia(path, false)
	if err != nil {
		t.Fatalf("readMedia: %v", err)
	}
	if m.duration != 0 || m.title != "" || m.hasArt {
		t.Errorf("read %+v out of a file that says nothing", m)
	}
}

// A real FLAC's length comes from its STREAMINFO block.
func TestReadFLACDuration(t *testing.T) {
	path := "../player/testdata/seek.flac"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no FLAC fixture: %v", err)
	}
	m, err := readMedia(path, false)
	if err != nil {
		t.Fatal(err)
	}
	// 219988 samples at 44100 Hz.
	if m.duration < 4900 || m.duration > 5100 {
		t.Errorf("duration %d ms", m.duration)
	}
}

// A real MP3's length comes from the Info header its first frame carries.
func TestReadMP3Duration(t *testing.T) {
	path := "../player/testdata/seek.mp3"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no MP3 fixture: %v", err)
	}
	m, err := readMedia(path, false)
	if err != nil {
		t.Fatal(err)
	}
	// 193 frames of 1152 samples at 44.1 kHz.
	if m.duration < 4900 || m.duration > 5200 {
		t.Errorf("duration %d ms", m.duration)
	}
}

// An MP3 whose tags are not read still measures itself by its bit rate.
func TestMP3LengthFromBitrate(t *testing.T) {
	// One MPEG-1 layer III frame at 128 kbps, 44.1 kHz, and 160 KB of
	// music in its wake: 128 kbps is 16 KB a second.
	frame := []byte{0xff, 0xfb, 0x90, 0x00}
	body := append(frame, bytes.Repeat([]byte{0}, 160_000)...)
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := readMedia(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.duration < 9000 || m.duration > 11000 {
		t.Errorf("duration %d ms", m.duration)
	}
}

// mp3Tag writes an ID3v2.3 tag holding the frames given, each of a name
// and its body.
func mp3Tag(frames map[string][]byte) []byte {
	var body []byte
	for id, payload := range frames {
		hdr := make([]byte, 10)
		copy(hdr, id)
		binary.BigEndian.PutUint32(hdr[4:], uint32(len(payload)))
		body = append(body, hdr...)
		body = append(body, payload...)
	}
	tag := []byte("ID3\x03\x00\x00")
	size := len(body)
	tag = append(tag, byte(size>>21), byte(size>>14), byte(size>>7), byte(size))
	return append(tag, body...)
}

// textFrame is an ID3v2 frame body of text, written as UTF-8.
func textFrame(s string) []byte { return append([]byte{3}, s...) }

// The tag's text names the track, whichever way it is written, and the
// cover inside it is found.
func TestReadMP3TagsAndArt(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4}
	apic := append([]byte{0, 'i', 'm', 'a', 'g', 'e', '/', 'p', 'n', 'g', 0, 3, 0}, png...)
	path := filepath.Join(t.TempDir(), "song.mp3")
	body := append(mp3Tag(map[string][]byte{
		"TIT2": textFrame("春日影"),
		"TPE1": textFrame("MyGO!!!!!"),
		"TALB": textFrame("迷跡波"),
		"TRCK": textFrame("3/12"),
		"APIC": apic,
	}), []byte{0xff, 0xfb, 0x90, 0x00}...)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := readMedia(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if m.title != "春日影" || m.artist != "MyGO!!!!!" || m.album != "迷跡波" || m.no != 3 {
		t.Errorf("read %+v", m)
	}
	if !m.hasArt || !bytes.Equal(m.art, png) {
		t.Errorf("art %d bytes, want the PNG inside", len(m.art))
	}
	// Without the art asked for, only that there is one is read.
	m, err = readMedia(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if !m.hasArt || len(m.art) != 0 {
		t.Errorf("hasArt %v with %d bytes", m.hasArt, len(m.art))
	}
}

// The oldest tags, at the end of the file, name the track too.
func TestReadID3v1(t *testing.T) {
	tag := make([]byte, id3v1Size)
	copy(tag, "TAG")
	copy(tag[3:33], "宝船")
	copy(tag[33:63], "DOUDOU")
	copy(tag[63:93], "海屿你")
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, append(make([]byte, 4096), tag...), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := readMedia(path, false)
	if err != nil {
		t.Fatal(err)
	}
	// The bytes are Latin-1, and Chinese written that way is what it is.
	if m.artist != "DOUDOU" || m.album != "海屿你" {
		t.Errorf("read %+v", m)
	}
}

func TestSplitName(t *testing.T) {
	for _, c := range []struct {
		name, artist, title string
		no                  int
	}{
		{"Artist - Title.mp3", "Artist", "Title", 0},
		{"01 - Artist - Title.flac", "Artist", "Title", 1},
		{"01. Artist - Title.mp3", "Artist", "Title", 1},
		{"01 Title.mp3", "", "Title", 1},
		{"Title.mp3", "", "Title", 0},
		// A name of two parts reads as artist and title, whatever they
		// are: nothing tells a year from a band.
		{"2019 - Live.mp3", "2019", "Live", 0},
		{"7.mp3", "", "7", 0},
	} {
		no, artist, title := splitName(c.name)
		if no != c.no || artist != c.artist || title != c.title {
			t.Errorf("%s: %d %q %q, want %d %q %q", c.name, no, artist, title, c.no, c.artist, c.title)
		}
	}
}

// A folder is listed in album and track order, and what it lists plays
// from the files it names.
func TestFolder(t *testing.T) {
	dir := t.TempDir()
	album := filepath.Join(dir, "吹奏楽")
	if err := os.MkdirAll(album, 0o700); err != nil {
		t.Fatal(err)
	}
	song := func(path string, tag map[string][]byte) {
		t.Helper()
		// One 128 kbps frame, and 32 KB of music behind it: 32 KB is two
		// seconds of it, and a file with no music at all names none.
		body := append(mp3Tag(tag), []byte{0xff, 0xfb, 0x90, 0x00}...)
		body = append(body, bytes.Repeat([]byte{0}, 32_000)...)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	song(filepath.Join(album, "02 - 迷跡波.mp3"), map[string][]byte{
		"TIT2": textFrame("迷跡波"), "TPE1": textFrame("MyGO!!!!!"), "TALB": textFrame("迷跡波"), "TRCK": textFrame("2"),
	})
	song(filepath.Join(album, "01 - 春日影.mp3"), map[string][]byte{
		"TIT2": textFrame("春日影"), "TPE1": textFrame("MyGO!!!!!"), "TALB": textFrame("迷跡波"), "TRCK": textFrame("1"),
	})
	// A file that says nothing of itself is named by its name alone, and
	// comes after the albums.
	if err := os.WriteFile(filepath.Join(dir, "loose.flac"), []byte("fLaC\x00\x00\x00\x22"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"notes.txt", ".hidden.mp3"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	tracks, err := Folder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 3 {
		t.Fatalf("listed %d tracks, want 3", len(tracks))
	}
	if tracks[0].Name != "春日影" || tracks[1].Name != "迷跡波" || tracks[2].Name != "loose" {
		t.Fatalf("album order: %q, %q, %q", tracks[0].Name, tracks[1].Name, tracks[2].Name)
	}
	if got := artistOf(tracks[0]); got != "MyGO!!!!!" {
		t.Errorf("artist %q", got)
	}
	if tracks[0].Album.Name != "迷跡波" || tracks[0].Duration == 0 {
		t.Errorf("album %q, duration %d", tracks[0].Album.Name, tracks[0].Duration)
	}
	for _, tr := range tracks {
		if tr.ID >= 0 {
			t.Errorf("%q has ID %d, want a negative one of its own", tr.Name, tr.ID)
		}
		if !tr.Playable() {
			t.Errorf("%q does not play", tr.Name)
		}
		if path, ok := LocalPath(tr.PlayURL); !ok || path != tr.PlayURL {
			t.Errorf("%q plays from %q, which is no file", tr.Name, tr.PlayURL)
		}
		if _, err := os.Stat(tr.PlayURL); err != nil {
			t.Errorf("%q plays from %q: %v", tr.Name, tr.PlayURL, err)
		}
	}
	if tracks[2].ID != ID(tracks[2].PlayURL) {
		t.Error("a track's ID is not the ID of its file")
	}
}

func TestFolderErrors(t *testing.T) {
	if _, err := Folder(""); err == nil {
		t.Error("listing no folder at all")
	}
	empty := t.TempDir()
	if _, err := Folder(empty); err == nil {
		t.Error("listing a folder of no music")
	}
}

// The lyrics of a track are the .lrc file beside it.
func TestLyrics(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "01 - Artist - Title.mp3")
	if err := os.WriteFile(song, []byte{0xff, 0xfb, 0x90, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "01 - Artist - Title.lrc"), []byte("[00:01.00]きみの\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tracks, err := Folder(dir)
	if err != nil {
		t.Fatal(err)
	}
	lrc, err := Lyrics(&tracks[0])
	if err != nil || !strings.Contains(lrc, "きみの") {
		t.Errorf("lyrics %q: %v", lrc, err)
	}
	// A track from an address has none, and asking is not an error.
	remote := tracks[0]
	remote.PlayURL = "https://example.com/song.mp3"
	if lrc, err := Lyrics(&remote); err != nil || lrc != "" {
		t.Errorf("lyrics of a remote track: %q %v", lrc, err)
	}
}

// The art inside a file is read when the cover is asked for.
func TestBytesArt(t *testing.T) {
	jpg := []byte{0xff, 0xd8, 0xff, 0xe0, 9, 9, 9}
	apic := append([]byte{0, 'i', 'm', 'a', 'g', 'e', '/', 'j', 'p', 'e', 'g', 0, 3, 0}, jpg...)
	path := filepath.Join(t.TempDir(), "song.mp3")
	body := append(mp3Tag(map[string][]byte{"APIC": apic}), []byte{0xff, 0xfb, 0x90, 0x00}...)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Bytes(ArtURL(path))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, jpg) {
		t.Errorf("read %d bytes of cover, want the JPEG inside", len(got))
	}
	// A cover beside the file is read as it is.
	side := filepath.Join(filepath.Dir(path), "cover.jpg")
	if err := os.WriteFile(side, jpg, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := Bytes(side); err != nil || !bytes.Equal(got, jpg) {
		t.Errorf("sidecar cover: %d bytes, %v", len(got), err)
	}
	if _, err := Bytes(ArtURL(side)); err == nil {
		t.Error("a cover was read out of an image file")
	}
}

func TestLocalPath(t *testing.T) {
	for _, c := range []struct {
		url  string
		path string
		ok   bool
	}{
		{"", "", false},
		{"https://example.com/a.mp3", "", false},
		{"http://example.com/a.mp3", "", false},
		{"file:///C:/music/a.mp3", filepath.FromSlash("C:/music/a.mp3"), true},
		{"/home/me/a.mp3", "/home/me/a.mp3", true},
	} {
		path, ok := LocalPath(c.url)
		if ok != c.ok || (ok && path != c.path) {
			t.Errorf("%q: %q %v, want %q %v", c.url, path, ok, c.path, c.ok)
		}
	}
}

// A source answers with a list, or with an object holding one, and only
// the tracks that say where they play from are kept.
func TestAPITracks(t *testing.T) {
	for _, body := range []string{
		`[{"name":"一","url":"https://x/1.mp3"}]`,
		`{"tracks":[{"name":"一","url":"https://x/1.mp3"}]}`,
		`{"songs":[{"title":"一","playUrl":"https://x/1.mp3","artist":"甲","duration":210}]}`,
		`{"data":[{"name":"一","play_url":"https://x/1.mp3","artists":["甲","乙"]}]}`,
		`{"result":{"tracks":[{"name":"一","url":"https://x/1.mp3"}]}}`,
	} {
		listed, err := apiTracks([]byte(body))
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if len(listed) != 1 {
			t.Fatalf("%s: %d tracks", body, len(listed))
		}
		tr, ok := listed[0].track(0)
		if !ok || tr.PlayURL != "https://x/1.mp3" || tr.Name != "一" {
			t.Errorf("%s: %+v", body, tr)
		}
		if tr.ID >= 0 || !tr.Playable() {
			t.Errorf("%s: ID %d, playable %v", body, tr.ID, tr.Playable())
		}
	}
	// A track that says where it plays from nowhere is left out.
	if tr, ok := (apiTrack{Name: "一"}).track(0); ok {
		t.Errorf("kept a track with nothing to play: %+v", tr)
	}
	if _, err := apiTracks([]byte("not json")); err == nil {
		t.Error("read tracks out of something that is not JSON")
	}
}

// Seconds are read as seconds: no song is a second long.
func TestDurationMs(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want int64
	}{
		{0, 0},
		{-1, 0},
		{210, 210000},
		{214000, 214000},
	} {
		if got := durationMs(c.in); got != c.want {
			t.Errorf("durationMs(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

// What a source refuses with is kept in the error: a card key that has run
// out, a song it cannot resolve — the only thing that says why nothing plays.
func TestAPIRefusalIsKept(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, `{"error":"全部解析失败：YouTube 风控"}`)
	}))
	defer srv.Close()

	_, err := API(srv.URL + "/songs")
	if err == nil {
		t.Fatal("API() took a refusal as a list of tracks")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "全部解析失败") {
		t.Errorf("API() said %q, want the status and what the source said", err)
	}
}

// A source that answers properly is read, seconds and all.
func TestAPIList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[{"name":"一","artist":"甲","duration":239,"url":"http://x/1.mp3"}]`)
	}))
	defer srv.Close()

	tracks, err := API(srv.URL + "/songs?preset=one")
	if err != nil {
		t.Fatalf("API() = %v", err)
	}
	if len(tracks) != 1 || tracks[0].Name != "一" || !tracks[0].Playable() {
		t.Fatalf("API() = %+v", tracks)
	}
	if tracks[0].Duration != 239000 {
		t.Errorf("Duration = %d, want 239000", tracks[0].Duration)
	}
}

// An address is tidied into what a request may carry: the scheme a
// browser would add, and the characters it hides — so that what is shown
// in a browser is what works here.
func TestNormalizeAddress(t *testing.T) {
	for _, c := range []struct {
		what string
		in   string
		want string
	}{
		{"no scheme", "127.0.0.1:3000/search?keywords=x", "http://127.0.0.1:3000/search?keywords=x"},
		{"no scheme, a name", "music-api.gdstudio.xyz/api.php?types=search&name=海阔天空",
			"http://music-api.gdstudio.xyz/api.php?types=search&name=%E6%B5%B7%E9%98%94%E5%A4%A9%E7%A9%BA"},
		{"quoted", `"https://a/b"`, "https://a/b"},
		{"spaces", "  https://a/b  ", "https://a/b"},
		{"scheme added by the browser", "//a/b", "http://a/b"},
		{"a name", "https://a/search?keywords=海阔天空&limit=2",
			"https://a/search?keywords=%E6%B5%B7%E9%98%94%E5%A4%A9%E7%A9%BA&limit=2"},
		{"a space", "https://a/s?q=a b", "https://a/s?q=a%20b"},
		{"already encoded", "https://a/s?q=%E6%B5%B7", "https://a/s?q=%E6%B5%B7"},
		{"another scheme", "ftp://a/b", "ftp://a/b"},
		{"nothing", "   ", ""},
	} {
		if got := normalizeAddress(c.in); got != c.want {
			t.Errorf("%s: %q became %q, want %q", c.what, c.in, got, c.want)
		}
	}
}
