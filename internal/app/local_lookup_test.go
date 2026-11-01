package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// serveBytes serves b at a new address, of the kind given, and returns it.
func serveBytes(t *testing.T, kind string, b []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", kind)
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/cover.jpg"
}

// fileSong is a file of this machine, kept where a folder of music is
// read from, with the tags the app reads it by.
func fileSong(t *testing.T, dir, name, artist, ext string) netease.Track {
	t.Helper()
	path := filepath.Join(dir, name+"-"+artist+ext)
	if err := os.WriteFile(path, []byte("fLaC\x00\x00\x00\x22"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := netease.Track{ID: source.ID(path), Name: name, PlayURL: path}
	tr.Artists = []netease.Artist{{Name: artist}}
	return tr
}

// lookupSong makes NetEase answer a search with the songs given, as it
// would for a title it knows.
func lookupSong(t *testing.T, songs ...netease.Track) {
	t.Helper()
	before := searchSongs
	searchSongs = func(query string, limit int) ([]netease.Track, error) {
		return songs, nil
	}
	t.Cleanup(func() { searchSongs = before })
}

// A file of this machine names no song of NetEase's: its id is one the app
// made up, which no account knows. Its cover and its lyrics come from the
// song it is, found by the name and the singer it was read as — before
// this, playing one showed neither, since the app asked NetEase about an
// id it had never given.
func TestLocalFileFindsItsSong(t *testing.T) {
	matched := netease.Track{ID: 42, Name: "恋人"}
	matched.Artists = []netease.Artist{{Name: "李荣浩"}}
	matched.Album = netease.Album{PicURL: "http://cdn.test/cover.jpg"}
	lookupSong(t, matched)

	a := newTestApp()
	a.settings = defaultSettings()
	tr := fileSong(t, t.TempDir(), "恋人", "李荣浩", ".flac")
	got := a.findSong(&tr)
	if got == nil {
		t.Fatal("the file found no song")
	}
	if got.ID != 42 {
		t.Errorf("found the song %d, want 42", got.ID)
	}
}

// A search answers with covers of the same title by other singers: a file
// takes the one it is, and none at all rather than another's.
func TestLocalFileTakesItsOwnSinger(t *testing.T) {
	cover := netease.Track{ID: 1, Name: "恋人"}
	cover.Artists = []netease.Artist{{Name: "别的歌手"}}
	own := netease.Track{ID: 2, Name: "恋人"}
	own.Artists = []netease.Artist{{Name: "李荣浩"}}
	lookupSong(t, cover, own)

	a := newTestApp()
	a.settings = defaultSettings()
	tr := fileSong(t, t.TempDir(), "恋人", "李荣浩", ".flac")
	if got := a.findSong(&tr); got == nil || got.ID != 2 {
		t.Errorf("found %+v, want the song by 李荣浩", got)
	}

	// A title nobody of that name sings is left alone.
	lookupSong(t, cover)
	other := fileSong(t, t.TempDir(), "没人唱过的歌", "无名", ".flac")
	if got := a.findSong(&other); got != nil {
		t.Errorf("took another song for it: %+v", got)
	}
}

// What a file is found to be is kept by the path it is at: a song that
// plays again asks NetEase nothing again.
func TestLocalMetaIsKeptByPath(t *testing.T) {
	matched := netease.Track{ID: 42, Name: "恋人"}
	matched.Artists = []netease.Artist{{Name: "李荣浩"}}
	matched.Album = netease.Album{PicURL: "http://cdn.test/cover.jpg"}
	lookupSong(t, matched)

	asked := 0
	before := searchSongs
	searchSongs = func(query string, limit int) ([]netease.Track, error) {
		asked++
		return []netease.Track{matched}, nil
	}
	t.Cleanup(func() { searchSongs = before })

	a := newTestApp()
	a.settings = defaultSettings()
	tr := fileSong(t, t.TempDir(), "恋人", "李荣浩", ".flac")
	a.localMeta(&tr)
	a.localMeta(&tr)
	if asked != 1 {
		t.Errorf("the song was looked up %d times for one file", asked)
	}
}

// A downloaded file's name carries the version it is — the show it was
// sung on, a remix — which the song itself is not titled by. It is asked
// for as it is, and then without that part, rather than left with no
// cover and no lyrics at all.
func TestLocalFileFindsSongPastItsVersion(t *testing.T) {
	plain := netease.Track{ID: 7, Name: "爱你"}
	plain.Artists = []netease.Artist{{Name: "徐俊雅"}}

	var asked []string
	before := searchSongs
	searchSongs = func(query string, limit int) ([]netease.Track, error) {
		asked = append(asked, query)
		// Only the plain title answers: the name with its version in
		// brackets is one NetEase does not know.
		if strings.HasPrefix(query, "爱你（美好爱情）") {
			return nil, nil
		}
		return []netease.Track{plain}, nil
	}
	t.Cleanup(func() { searchSongs = before })

	a := newTestApp()
	a.settings = defaultSettings()
	tr := fileSong(t, t.TempDir(), "爱你（美好爱情）", "徐俊雅", ".mp3")
	got := a.findSong(&tr)
	if got == nil || got.ID != 7 {
		t.Fatalf("found %+v; asked %q", got, asked)
	}
	if len(asked) < 2 {
		t.Errorf("asked only %q: the version in the name was not tried without", asked)
	}
}

// The sidecars the page offers to fetch are found the same way: a file
// with no cover beside it gets the one of the song it is.
func TestWriteSidecarsFindsTheSong(t *testing.T) {
	dir := t.TempDir()
	cover := []byte{0xFF, 0xD8, 0xFF, 0xE0, 9, 9}
	srv := serveBytes(t, "image/jpeg", cover)

	matched := netease.Track{ID: 42, Name: "恋人"}
	matched.Artists = []netease.Artist{{Name: "李荣浩"}}
	matched.Album = netease.Album{PicURL: srv}
	lookupSong(t, matched)

	a := newTestApp()
	a.settings = defaultSettings()
	tr := fileSong(t, dir, "恋人", "李荣浩", ".flac")
	if err := a.writeSidecars(&tr); err != nil {
		t.Fatalf("no sidecar could be written: %v", err)
	}
	base := strings.TrimSuffix(tr.PlayURL, filepath.Ext(tr.PlayURL))
	got, err := os.ReadFile(base + ".jpg")
	if err != nil {
		t.Fatalf("the cover was not written beside the file: %v", err)
	}
	if len(got) != len(cover) {
		t.Errorf("the cover is %d bytes, want %d", len(got), len(cover))
	}
}
