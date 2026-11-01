package source

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"netease-music-mygo/internal/lxsource"
	"netease-music-mygo/internal/netease"
)

// SearchMark is where a source's address takes the words to search for:
// the address is filled in with them and asked, every time something is
// played that the address is to find. Such a source lists nothing of its
// own, which is what makes it a source to fall back on rather than one to
// read through.
const SearchMark = "{keyword}"

// searchParams are the query parameters the interfaces in use name the
// words to search for by: an address copied from a browser's search box
// carries one, holding the song it was searched for, which is where the
// words go instead.
var searchParams = []string{"keywords", "keyword", "kw", "q", "wd", "word", "search", "query", "name", "k"}

// LevelBest asks a source for the best it offers, rather than for one of
// the levels NetEase names: a script says which qualities it has for a
// platform, and the best of them is what a listener who asks for the best
// wants. It is not a level of NetEase's, and only a source is asked for
// it.
const LevelBest = "best"

// scriptPlatform is the platform a Luoxue (LX) script resolves NetEase's
// own songs under. The songs asked about here are NetEase's — they are
// the ones NetEase would not serve — so it is always this one.
const scriptPlatform = "wy"

// Answer is where a song plays instead, and who found it: the address,
// the source that answered with it, and the level it was asked at, named
// as NetEase names levels. The level is empty for a level NetEase does
// not have, and for a source that searched for the song by name, which
// says nothing about what it found.
type Answer struct {
	URL    string
	Source string
	Level  string
}

// Searchable says whether the entry is one to ask for the words to search
// for: an address carrying {keyword}, or one whose address already names a
// song to search for, as the search a browser made leaves behind.
func (e Entry) Searchable() bool {
	if !e.On() || e.Kind == KindScript {
		return false
	}
	return e.SearchMarked() || searchParamOf(e.Value) != ""
}

// SearchMarked says whether the address carries {keyword} itself, which is
// what a listener wrote to have a source asked for the words every time:
// an address that only happens to hold the words of one search is searched
// with too, but it is not what a song with no stream falls back on.
func (e Entry) SearchMarked() bool {
	return e.On() && e.Kind != KindScript && strings.Contains(e.Value, SearchMark)
}

// searchParamOf returns the query parameter of an address that names what
// to search for, if it has one: the address of a search a browser made,
// pasted in whole, still holds the words it searched for.
func searchParamOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	q := u.Query()
	for _, name := range searchParams {
		if strings.TrimSpace(q.Get(name)) != "" {
			return name
		}
	}
	return ""
}

// SearchAddr is the entry's address with the words to search for in place
// of its {keyword}: the words are escaped the way an address carries
// them, which is what a browser would send.
func SearchAddr(e Entry, keyword string) (string, bool) {
	raw := strings.TrimSpace(e.Value)
	words := strings.TrimSpace(keyword)
	if !strings.Contains(raw, SearchMark) {
		// An address that already holds a search's words takes the new
		// ones in their place: a browser's own search box is where the
		// address came from, and a listener who pasted it meant to search
		// with it, not to be stuck on the song it was left holding.
		name := searchParamOf(raw)
		if name == "" {
			return "", false
		}
		u, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		q := u.Query()
		q.Set(name, words)
		u.RawQuery = q.Encode()
		return normalizeAddress(u.String()), true
	}
	return normalizeAddress(strings.ReplaceAll(raw, SearchMark, escapeURL(words))), true
}

// Searchable says whether any of the entries can be searched: an address
// that takes the words to search for. Only those can: a folder lists what
// is on this machine, a plain endpoint lists what it holds, and a script
// has no search in it at all — its protocol answers for one song at a
// time, by the id NetEase gave it.
func Searchable(entries []Entry) bool {
	for _, e := range entries {
		if e.Searchable() {
			return true
		}
	}
	return false
}

// Search asks every source that can be searched for the words, and lists
// what they answer with: the songs are the interfaces' own, from
// wherever they keep them, and come back with the source that found them.
// A source that cannot answer is left out, and one that fails says why.
func Search(entries []Entry, words string) ([]netease.Track, []string) {
	words = strings.TrimSpace(words)
	var tracks []netease.Track
	var failed []string
	seen := make(map[string]bool)
	for _, e := range entries {
		if !e.Searchable() {
			continue
		}
		addr, ok := SearchAddr(e, words)
		if !ok {
			continue
		}
		listed, err := listedBy(e.Kind, addr)
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

// Fallback finds where a song NetEase will not serve can be played
// instead, from the sources the listener keeps: each is asked in turn,
// and the first that answers with an address wins.
//
// Two kinds answer. A script is asked for the song by its NetEase id,
// which is enough: it keeps NetEase's own library. An address carrying
// {keyword} is asked for the song by name and singer. A source that is
// neither — a folder, a plain endpoint, a playlist — is passed over, as
// there is nothing to ask it about a particular song.
func Fallback(entries []Entry, want netease.Track, level string) (Answer, error) {
	asked := 0
	var why []string
	for _, e := range entries {
		switch {
		case e.Kind == KindScript && e.On():
			asked++
			url, got, err := byScript(e, want, level)
			if err != nil {
				why = append(why, e.Title()+"："+err.Error())
				continue
			}
			return Answer{URL: url, Source: e.Title(), Level: got}, nil
		case e.Searchable():
			asked++
			url, err := bySearch(e, want)
			if err != nil {
				why = append(why, e.Title()+"："+err.Error())
				continue
			}
			return Answer{URL: url, Source: e.Title()}, nil
		}
	}
	switch {
	case asked == 0:
		return Answer{}, errors.New("还没有能替它取流的音源：加一个「音源文件（洛雪 JS）」，或者一条带 " + SearchMark + " 的地址")
	case len(why) == 0:
		return Answer{}, errors.New("这些音源里也没有这首歌")
	}
	return Answer{}, errors.New(strings.Join(why, "；"))
}

// byScript asks a Luoxue (LX) audio source script where the song plays,
// and answers with the address and the level it was asked at. The level
// is what the script was asked for, which at LevelBest is the best it
// says it has.
func byScript(e Entry, want netease.Track, level string) (url, got string, err error) {
	box, err := lxsource.Load(strings.TrimSpace(e.Value))
	if err != nil {
		return "", "", err
	}
	have := box.Platforms()
	if !slices.Contains(have, scriptPlatform) {
		if len(have) == 0 {
			return "", "", errors.New("这个音源脚本没有声明任何平台")
		}
		return "", "", fmt.Errorf("这个音源脚本不解析网易云（%s），它只会 %s", scriptPlatform, strings.Join(have, "、"))
	}
	// What to ask the script for, best first: the qualities the script
	// says it has, from the best down, or the one level the listener
	// asked for. A quality the script cannot give is answered with a
	// refusal — a card key that ran out, a song it does not have — so
	// the next one down is asked, and the first that answers wins.
	var asked []string
	if level == LevelBest {
		asked = qualityOrder(box.Qualitys(scriptPlatform))
	} else {
		asked = []string{lxsource.Quality(level)}
	}
	var last error
	for _, quality := range asked {
		url, err = box.URL(scriptPlatform, quality, map[string]any{
			"musicId":   want.ID,
			"id":        want.ID,
			"name":      want.Name,
			"singer":    want.ArtistNames(),
			"albumId":   want.Album.ID,
			"albumName": want.Album.Name,
		})
		if err == nil {
			return url, levelOfQuality(quality), nil
		}
		last = err
		// The listener asked for one level: no others are tried.
		if level != LevelBest {
			break
		}
	}
	if last == nil {
		last = errors.New("这个音源脚本没有可用的音质")
	}
	return "", "", last
}

// qualityOrder is the qualities a script says it has, from the best down:
// what it is asked for, one after another, until one answers. A quality
// the script offers that NetEase has no name for comes before the ones it
// does, as a script that offers one offers something better than 320k.
func qualityOrder(have []string) []string {
	rank := func(q string) int {
		switch q {
		case "flac24bit", "hires":
			return 0
		case "flac", "lossless":
			return 1
		case "320k":
			return 2
		case "192k":
			return 3
		case "128k":
			return 4
		}
		// One NetEase has no name for: ahead of everything it does name,
		// as nothing a script offers is worse than what it does.
		return -1
	}
	out := append([]string(nil), have...)
	sort.SliceStable(out, func(a, b int) bool { return rank(out[a]) < rank(out[b]) })
	// A script that says nothing about its qualities is asked for the
	// best that is usual, so that it is not asked for nothing at all.
	if len(out) == 0 {
		return []string{"flac24bit", "flac", "320k", "192k", "128k"}
	}
	// Every level NetEase names goes after what the script named, from the
	// best down, for a script that answers a quality it did not declare.
	return append(out, "flac24bit", "flac", "320k", "192k", "128k")
}

// levelOfQuality is the level NetEase names a script's quality by, or ""
// for one it does not name.
func levelOfQuality(quality string) string {
	switch quality {
	case "flac24bit", "hires":
		return "hires"
	case "flac", "lossless":
		return "lossless"
	case "320k":
		return "exhigh"
	case "192k":
		return "higher"
	case "128k":
		return "standard"
	}
	return ""
}

// bySearch asks an address that takes the words to search for, and takes
// the first address it answers with.
func bySearch(e Entry, want netease.Track) (string, error) {
	keyword := strings.TrimSpace(want.Name + " " + want.ArtistNames())
	addr, ok := SearchAddr(e, keyword)
	if !ok {
		return "", errors.New("地址里没有 " + SearchMark)
	}
	tracks, err := listedBy(e.Kind, addr)
	if err != nil {
		return "", err
	}
	for _, t := range tracks {
		if strings.TrimSpace(t.PlayURL) != "" {
			return t.PlayURL, nil
		}
	}
	return "", errors.New("搜到了，但没有能播的地址")
}

// listedBy lists what an address holds, read as the kind of source the
// address was filled in under.
func listedBy(kind, addr string) ([]netease.Track, error) {
	if kind == KindLink {
		return Link(addr)
	}
	return API(addr)
}

// ScriptInfo describes a Luoxue (LX) source script: what it says it can
// resolve, for the settings to show beside the file it was read from.
func ScriptInfo(path string) (string, error) {
	box, err := lxsource.Load(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	return box.Describe(), nil
}
