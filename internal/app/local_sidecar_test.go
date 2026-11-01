package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/netease"
)

// A song saved as a bare audio stream has no cover and no lyrics
// beside it, which is what a folder of this machine is read for: the
// page offers to fetch what is missing, and writes it where it reads.
func TestLocalSidecarsAreFetched(t *testing.T) {
	cover := []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3, 4}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(cover)
	}))
	defer srv.Close()

	dir := t.TempDir()
	song := filepath.Join(dir, "歌手 - 歌名.mp3")
	if err := os.WriteFile(song, []byte("ID3 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := netease.Track{
		ID: -1, Name: "歌名", PlayURL: song,
		Album: netease.Album{PicURL: srv.URL + "/cover.jpg"},
	}
	if !songNeedsSidecars(&tr) {
		t.Fatal("a song with no cover beside it is taken for one that has")
	}

	a := newTestApp()
	a.settings = defaultSettings()
	if err := a.writeSidecars(&tr); err != nil {
		t.Fatal(err)
	}
	base := song[:len(song)-len(filepath.Ext(song))]
	got, err := os.ReadFile(base + ".jpg")
	if err != nil {
		t.Fatalf("the cover was not written beside the song: %v", err)
	}
	if len(got) != len(cover) {
		t.Errorf("the cover beside it is %d bytes, want %d", len(got), len(cover))
	}
	// The cover is there now; the lyrics are not, because this song has
	// none to fetch: it is not one NetEase knows.
	if _, err := os.Stat(base + ".lrc"); err == nil {
		t.Error("a lyrics file appeared for a song that has none")
	}
}

// What a cover is written as follows its bytes, so that a PNG is not
// saved as a JPEG that no reader opens.
func TestCoverExt(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if got := coverExt(png); got != ".png" {
		t.Errorf("a PNG is written as %q", got)
	}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	if got := coverExt(jpeg); got != ".jpg" {
		t.Errorf("a JPEG is written as %q", got)
	}
}
