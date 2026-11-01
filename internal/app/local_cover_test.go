package app

import (
	"os"
	"testing"

	"netease-music-mygo/internal/netease"
)

// The cover found for a file of this machine is put where the views read
// it: they draw the address a track carries, so the bytes are written
// where an address names them, and the track takes that address. Before
// this, the bytes went only to the system's Now Playing, and the app's own
// views showed nothing.
func TestLocalCoverReachesTheTrack(t *testing.T) {
	dir := t.TempDir()
	tr := fileSong(t, dir, "恋人", "李荣浩", ".flac")
	if tr.Album.PicURL != "" {
		t.Fatal("the test's file already names a cover")
	}
	cover := []byte{0xFF, 0xD8, 0xFF, 0xE0, 7, 7, 7}

	a := newTestApp()
	a.settings = defaultSettings()
	a.showLocalCover(&tr, tr.PlayURL, cover)

	if tr.Album.PicURL == "" {
		t.Fatal("the track still names no cover")
	}
	got, err := os.ReadFile(tr.Album.PicURL)
	if err != nil {
		t.Fatalf("the cover it names is not there: %v", err)
	}
	if len(got) != len(cover) {
		t.Errorf("the cover is %d bytes, want %d", len(got), len(cover))
	}
}

// A file that names a cover already keeps it: one is not fetched over it.
func TestLocalCoverLeavesOneItHas(t *testing.T) {
	tr := netease.Track{ID: -1, Name: "恋人"}
	tr.PlayURL = "D:/音乐/恋人.flac"
	tr.Album.PicURL = "art://D:/音乐/恋人.flac"

	a := newTestApp()
	a.settings = defaultSettings()
	a.showLocalCover(&tr, tr.PlayURL, []byte{0xFF, 0xD8, 0xFF})
	if tr.Album.PicURL != "art://D:/音乐/恋人.flac" {
		t.Errorf("the cover it had became %q", tr.Album.PicURL)
	}
}

// The names a title is asked for by: as it is, and then without the
// bracketed part a download appends.
func TestTitleForms(t *testing.T) {
	for _, c := range []struct {
		name string
		want []string
	}{
		{"恋人", []string{"恋人"}},
		{"爱你（美好爱情）", []string{"爱你（美好爱情）", "爱你"}},
		{"Ah! (Live)", []string{"Ah! (Live)", "Ah!"}},
		{"晴天 [Remix]", []string{"晴天 [Remix]", "晴天"}},
	} {
		got := titleForms(c.name)
		if len(got) != len(c.want) {
			t.Errorf("%q asked as %q, want %q", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q asked as %q, want %q", c.name, got, c.want)
				break
			}
		}
	}
}
