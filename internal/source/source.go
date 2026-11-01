// Package source plays music that is not NetEase's: folders of files on
// this machine, HTTP endpoints that list their tracks with where each
// streams from, and links to a track, a playlist, or an endpoint. What it
// lists is what the player queues: netease.Tracks carrying their own
// PlayURL, which the player streams without asking NetEase for anything,
// and without an account.
//
// Several sources list at once: the settings keep a list of them, and
// what each lists is queued together, each track naming the source it
// came from. A source that fails leaves the others standing.
package source

import (
	"errors"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"

	"netease-music-mygo/internal/netease"
)

// The kinds of third-party source the app may play from, as the settings
// keep them.
const (
	KindOff    = "off"
	KindFolder = "folder"
	KindAPI    = "api"
	KindLink   = "link"
	// KindScript is a Luoxue (LX) audio source script kept on this
	// machine. It is not a library and lists nothing: it is asked where
	// a song NetEase will not serve plays instead.
	KindScript = "script"
)

// Kind is one kind of source that may be added, with what it is called
// on screen.
type Kind struct {
	Kind  string
	Label string
}

// Kinds are the kinds of source that may be added, in the order they are
// offered. Off is not one of them: it is the list holding nothing.
var Kinds = []Kind{
	{KindFolder, "本机文件夹"},
	{KindAPI, "HTTP 接口"},
	{KindLink, "链接"},
	{KindScript, "音源文件（洛雪 JS）"},
}

// artPrefix marks a cover that lives inside a music file, read out of its
// tags when the app asks for it: "art://<the file>".
const artPrefix = "art://"

// Entry is one third-party source: a folder of music on this machine, an
// endpoint that lists its tracks, or a link to a playlist or a track.
type Entry struct {
	// ID names the entry in the settings and in the widgets that edit it,
	// so that adding or removing one leaves the others where they are.
	ID string `json:"id"`
	// Name is what the entry is called on screen, and on the tracks it
	// lists. An entry with none is called after its kind.
	Name string `json:"name,omitempty"`
	// Kind is which of Kinds the entry is.
	Kind string `json:"kind"`
	// Value is where it lists from: a folder, an endpoint, or a link.
	Value string `json:"value"`
	// Enabled is whether the app lists it: an entry that is turned off
	// keeps where it points, and lists nothing.
	Enabled bool `json:"enabled"`
}

// On says whether the entry is one to list: turned on, of a kind the app
// knows, and naming where to list from.
func (e Entry) On() bool {
	return e.Enabled && strings.TrimSpace(e.Value) != "" && Label(e.Kind) != ""
}

// Lists says whether the entry lists songs of its own: a script does not,
// and neither does an address waiting for the words to search for. Both
// are still sources: they are asked for one song at a time, where the
// songs that are listed are played.
func (e Entry) Lists() bool {
	// An address that happens to carry the words of a search — one copied
	// from a browser — still lists what it answers with, and is searched
	// with besides: on the page it reads as a list until something is
	// searched.
	return e.On() && e.Kind != KindScript && !e.SearchMarked()
}

// Answers says whether the entry can be asked where one particular song
// plays: a source script kept on this machine, or an address carrying the
// words to search for. These are what a song NetEase will not serve falls
// back on, so a listener who keeps one has every song they can name.
func (e Entry) Answers() bool {
	return e.Kind == KindScript && e.On() || e.Searchable()
}

// AnswersSongs says whether any of the entries can be asked where a song
// plays: whether there is anything to fall back on at all.
func AnswersSongs(entries []Entry) bool {
	for _, e := range entries {
		if e.Answers() {
			return true
		}
	}
	return false
}

// Title is what the entry is called: its own name, or the kind it is.
func (e Entry) Title() string {
	if name := strings.TrimSpace(e.Name); name != "" {
		return name
	}
	if label := Label(e.Kind); label != "" {
		return label
	}
	return "音源"
}

// Label is what the kind of source is called on screen, or "" for one the
// app does not have.
func Label(kind string) string {
	for _, k := range Kinds {
		if k.Kind == kind {
			return k.Label
		}
	}
	return ""
}

// Load lists every source the entries name, in the order they are kept,
// and the names of those that failed with why: a source that cannot be
// listed leaves the others standing, as one endpoint being down is no
// reason to stop playing a folder of files. Tracks two sources both list
// are listed once.
func Load(entries []Entry) ([]netease.Track, []string) {
	var tracks []netease.Track
	var failed []string
	seen := make(map[string]bool)
	for _, e := range entries {
		if !e.Lists() {
			continue
		}
		listed, err := list(e)
		if err != nil {
			failed = append(failed, e.Title()+"："+err.Error())
			continue
		}
		for _, t := range listed {
			if t.PlayURL != "" {
				if seen[t.PlayURL] {
					continue
				}
				seen[t.PlayURL] = true
			}
			t.Source = e.Title()
			tracks = append(tracks, t)
		}
	}
	return tracks, failed
}

// list lists what one source holds.
func list(e Entry) ([]netease.Track, error) {
	switch e.Kind {
	case KindFolder:
		return Folder(e.Value)
	case KindAPI:
		return API(e.Value)
	case KindLink:
		return Link(e.Value)
	case KindScript:
		// A script lists nothing: it is not asked until there is a song
		// to ask about, and holding nothing is no failure.
		return nil, nil
	}
	return nil, errors.New("不认识这种音源：" + e.Kind)
}

// One lists what a single source holds, so that the settings can say
// whether an address works before it is played from: the same work Load
// does for a list of them, with the reason a source holds nothing said
// rather than counted.
func One(e Entry) ([]netease.Track, error) { return list(e) }

// ID names a third-party track by what names it — its file, or its
// address — with an ID of its own, negative, so that it is never one of
// NetEase's, whatever the source lists.
func ID(key string) int64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	return -int64(h.Sum64()>>1) - 1
}

// ArtURL names the cover inside the music file at path, for a track of
// this package to carry as its album's picture.
func ArtURL(path string) string { return artPrefix + path }

// normalizeAddress tidies an address as people paste it, so that what a
// browser shows is what works: the quotes and spaces a copy brings along
// are dropped, an address written without its scheme has http:// put in
// front — a browser adds it too — and the characters a browser hides,
// such as the Chinese name of a song, are percent-encoded, which is what
// the server was being asked for all along.
func normalizeAddress(raw string) string {
	s := strings.Trim(strings.TrimSpace(raw), "\"'“”‘’")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "//") {
		s = "http:" + s
	} else if i := strings.Index(s, "://"); i <= 0 || !isScheme(s[:i]) {
		s = "http://" + s
	}
	return escapeURL(s)
}

// isScheme says whether s names a scheme: the letters, digits and marks a
// URL scheme is written with.
func isScheme(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '+', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// urlMarks are what a URL may carry as it is, besides letters and digits:
// the braces are here for the {keyword} a source's address may hold.
const urlMarks = "-._~:/?#[]@!$&'()*+,;={}"

// escapeURL percent-encodes what a URL may not carry as it is — the bytes
// of a name written in Chinese, a space — and leaves what it may carry,
// including the escapes it already holds, as it is.
func escapeURL(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case strings.IndexByte(urlMarks, c) >= 0:
			b.WriteByte(c)
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b.WriteString(s[i : i+3])
			i += 2
		default:
			const hex = "0123456789ABCDEF"
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// LocalPath returns the path a stream names on this machine, if it names
// one: a file URL, or a path as it is. An http or https address is the
// source's to serve, and names no file here.
func LocalPath(u string) (string, bool) {
	switch {
	case u == "":
		return "", false
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"):
		return "", false
	case strings.HasPrefix(u, "file://"):
		p := strings.TrimPrefix(u, "file://")
		// file:///C:/music is a path with a slash before its drive.
		if len(p) > 2 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
		if p == "" {
			return "", false
		}
		return filepath.FromSlash(p), true
	default:
		return u, true
	}
}

// Bytes returns what is at u: a cover the image CDN serves, a picture on
// this machine, or the art inside a music file, named as ArtURL names it.
func Bytes(u string) ([]byte, error) {
	if u == "" {
		return nil, errors.New("没有封面")
	}
	if path, ok := strings.CutPrefix(u, artPrefix); ok {
		m, err := readMedia(path, true)
		if err != nil {
			return nil, err
		}
		if len(m.art) == 0 {
			return nil, errors.New("这首歌里没有内嵌封面")
		}
		return m.art, nil
	}
	if path, ok := LocalPath(u); ok {
		return os.ReadFile(path)
	}
	return netease.Fetch(u)
}

// Lyrics returns the lyrics of the third-party track t: the .lrc file
// beside the file it plays from, which the sources of this package may
// keep, as a sidecar. A track that streams from an address has none.
func Lyrics(t *netease.Track) (string, error) {
	path, ok := LocalPath(t.PlayURL)
	if !ok {
		return "", nil
	}
	b, err := os.ReadFile(strings.TrimSuffix(path, filepath.Ext(path)) + ".lrc")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
