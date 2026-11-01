package source

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"netease-music-mygo/internal/ffmpeg"
	"netease-music-mygo/internal/netease"
)

// A folder is read for the music kept in it, not only the two formats the
// app decodes: a library of one's own is usually in several, and the rest
// are listed so that what is on this machine is seen whole.
func TestFolderListsEveryMusicFormat(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"a.mp3", "b.flac", "c.m4a", "d.ogg", "e.wav", "f.wma", "g.aac", "h.opus",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Files that are no music at all are still left out.
	for _, n := range []string{"notes.txt", "cover.jpg", "song.lrc"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	tracks, err := Folder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != len(names) {
		t.Fatalf("listed %d files, want %d", len(tracks), len(names))
	}
	for _, n := range names {
		if !slices.ContainsFunc(tracks, func(tr netease.Track) bool { return filepath.Base(tr.PlayURL) == n }) {
			t.Errorf("%q is not listed", n)
		}
	}
}

// Only MP3 and FLAC are played: the rest are listed, and the app says so.
func TestDecodable(t *testing.T) {
	for _, c := range []struct {
		path string
		want bool
	}{
		{"a.mp3", true},
		{"A.MP3", true},
		{"a.flac", true},
		{"a.ogg", true},
		{"A.OGG", true},
		{"a.oga", true},
		{"a.wav", true},
		// The formats ffmpeg decodes: played where there is one, which
		// this machine has; a machine without it only lists them.
		{"a.m4a", ffmpeg.Available()},
		{"a.wma", ffmpeg.Available()},
		{"a.aac", ffmpeg.Available()},
		{"a.opus", ffmpeg.Available()},
	} {
		if got := Decodable(c.path); got != c.want {
			t.Errorf("%s decodable %v, want %v", c.path, got, c.want)
		}
	}
}

// A file of a format the app does not read is named by its file's name,
// and is not taken for an MP3 whose tags went unread.
func TestUnlistedFormatIsNamedByItsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "歌手 - 歌名.m4a")
	// An MP4's first bytes, which are no MP3's and no FLAC's.
	body := append([]byte{0, 0, 0, 0x20}, []byte("ftypM4A ")...)
	body = append(body, make([]byte, 64)...)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := readMedia(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.title != "" || m.artist != "" || m.duration != 0 {
		t.Errorf("read %+v from a format it does not read", m)
	}
	tracks, err := Folder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Name != "歌名" {
		t.Errorf("named it %+v", tracks)
	}
}
