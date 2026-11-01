package source

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"netease-music-mygo/internal/ffmpeg"
	"netease-music-mygo/internal/netease"
)

// musicExts are the extensions of the music a folder is read for: the
// formats the player decodes, and the formats a listener's own library is
// usually in, which are listed all the same so that what is on this
// machine is seen whole. Only the first are played; the others are marked
// as not playing, rather than left out as if they were not there.
var musicExts = []string{
	// Played, by the decoders the app has.
	".mp3", ".flac", ".ogg", ".oga", ".wav",
	// Played, by ffmpeg, where there is one on the machine.
	".m4a", ".aac", ".opus", ".wma", ".ape", ".aiff", ".aif", ".alac", ".wv",
}

// Decodable says whether the file at path is one the app plays: an MP3, a
// FLAC, an Ogg Vorbis or a WAV itself, or one of the formats ffmpeg
// decodes, which it is asked for where there is one.
func Decodable(path string) bool {
	if ffmpeg.Handled(path) {
		return ffmpeg.Available()
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3", ".flac", ".ogg", ".oga", ".wav":
		return true
	}
	return false
}

// Folder lists the music in a folder on this machine: every file of a
// format music is kept in under it, including its subfolders, each named
// by its tags or else by its file name, and playing from the file itself
// — no account, and no network, are needed for any of it.
func Folder(dir string) ([]netease.Track, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("还没有选择文件夹")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(root); err != nil {
		return nil, errors.New("读不到这个文件夹：" + root)
	} else if !info.IsDir() {
		return nil, errors.New("这不是一个文件夹：" + root)
	}
	var found []localTrack
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A folder that cannot be read is skipped: the rest of the
			// music is still worth listing.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// Hidden folders hold no music the listener put there.
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if ext := strings.ToLower(filepath.Ext(path)); slices.Contains(musicExts, ext) {
			found = append(found, localTrackOf(path))
		}
		return nil
	})
	if len(found) == 0 {
		return nil, errors.New("这个文件夹里没有认得出的音乐文件")
	}
	// By album, each folder's music together, then as the tracks number
	// themselves on each.
	group := func(t localTrack) string {
		if t.album != "" {
			return t.album + "\x00" + t.dir
		}
		// Music that names no album is grouped by the folder it is in,
		// after the albums, which are listed by name.
		return "\xff" + t.dir
	}
	slices.SortFunc(found, func(a, b localTrack) int {
		if c := strings.Compare(group(a), group(b)); c != 0 {
			return c
		}
		if a.no != b.no {
			// A track that numbers itself comes before one that does not.
			if a.no == 0 {
				return 1
			}
			if b.no == 0 {
				return -1
			}
			return a.no - b.no
		}
		return strings.Compare(a.track.Name, b.track.Name)
	})
	tracks := make([]netease.Track, len(found))
	for i, f := range found {
		tracks[i] = f.track
	}
	return tracks, nil
}

// localTrack is a file of a folder, before it is sorted in among the rest.
type localTrack struct {
	track netease.Track
	album string
	dir   string
	no    int
}

// localTrackOf makes the track the music file at path plays as.
func localTrackOf(path string) localTrack {
	no, artist, title := splitName(filepath.Base(path))
	t := netease.Track{
		ID:      ID(path),
		Name:    title,
		PlayURL: path,
	}
	if artist != "" {
		t.Artists = []netease.Artist{{Name: artist}}
	}
	m, err := readMedia(path, false)
	if err == nil {
		t.Name = firstOf(m.title, title)
		t.Duration = m.duration
		if m.artist != "" {
			t.Artists = []netease.Artist{{Name: m.artist}}
		}
		t.Album = netease.Album{Name: m.album, PicURL: localCover(path, m.hasArt)}
		no = firstInt(m.no, no)
	}
	return localTrack{track: t, album: t.Album.Name, dir: filepath.Dir(path), no: no}
}

// localCover returns where the cover of the music at path is: inside the
// file when it carries one, or else an image beside it.
func localCover(path string, hasArt bool) string {
	if hasArt {
		return ArtURL(path)
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		if info, err := os.Stat(base + ext); err == nil && !info.IsDir() {
			return base + ext
		}
	}
	for _, name := range []string{"cover.jpg", "folder.jpg", "front.jpg", "cover.png", "folder.png"} {
		p := filepath.Join(filepath.Dir(path), name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	// A cover fetched for the file before is kept in the app's cache, under
	// a name of the file's own: it is read back so that a restart shows the
	// covers it fetched, rather than fetching them all again.
	return cachedCover(path)
}

// coverCache is where the app keeps covers it fetched for files of this
// machine, under a name made of the file's path.
var coverCache string

// SetCoverCache names the folder covers fetched for files of this machine
// are kept in.
func SetCoverCache(dir string) { coverCache = dir }

// CachedCoverName is what a cover fetched for the file at path is kept
// as: the app writes it there, and a scan reads it back.
func CachedCoverName(path, ext string) string {
	return "local-" + strconv.FormatInt(ID(path), 16) + ext
}

// cachedCover returns the cover kept for the file at path, if one is.
func cachedCover(path string) string {
	if coverCache == "" {
		return ""
	}
	for _, ext := range []string{".jpg", ".png", ".webp"} {
		p := filepath.Join(coverCache, CachedCoverName(path, ext))
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// splitName reads a file's name as "Artist - Title", as most collections
// name their files, or as the title alone; a number in front of either is
// where the track sits on its album.
func splitName(name string) (no int, artist, title string) {
	rest := strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name)))
	if n, after, ok := leadingNumber(rest); ok {
		no, rest = n, strings.TrimSpace(after)
	}
	if a, t, ok := strings.Cut(rest, " - "); ok {
		a, t = strings.TrimSpace(a), strings.TrimSpace(t)
		// "01 - Artist - Title" and "Artist - 01 Title" both number the
		// track, and both were read above.
		if a != "" && t != "" {
			return no, a, t
		}
	}
	return no, "", rest
}

// leadingNumber reads a track number at the start of a file's name, as
// "01. ", "01 - " or "01 " write it.
func leadingNumber(s string) (int, string, bool) {
	i := 0
	for i < len(s) && i < 3 && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	// A name of three digits or more is a year or a word, not a number:
	// "2019 - Live" is not track 201.
	if i == 0 || i >= len(s) {
		return 0, s, false
	}
	switch s[i] {
	case ' ', '.', '-':
	default:
		return 0, s, false
	}
	rest := strings.TrimSpace(strings.TrimLeft(s[i:], " .-"))
	if rest == "" {
		return 0, s, false
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, s, false
	}
	return n, rest, true
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
