package source

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"netease-music-mygo/internal/netease"
)

// API lists the tracks of an HTTP source: an endpoint the listener runs,
// which answers with JSON. It may answer with a list of tracks, or with an
// object holding one under "tracks", "songs", "data" or "result":
//
//	{"tracks": [
//	   {"name": "歌名", "artist": "歌手", "album": "专辑",
//	    "duration": 214000, "url": "https://…/song.mp3", "cover": "https://…/cover.jpg"},
//	   …
//	]}
//
// Only the address each track streams from is required: a track without
// one is left out, since there is nothing to play. The names on the left
// are the ones read; "title" for "name", "artists" for "artist", "dt" for
// "duration", "playUrl" or "play_url" for "url", and "picUrl" or "pic"
// for "cover" are read too, for sources that write them that way.
//
// What an answer that lists nothing says about why — "请先登录", a card
// key that has run out — is said too, rather than leaving the listener
// with a silence.
func API(endpoint string) ([]netease.Track, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, errors.New("还没有填写接口地址")
	}
	// What is pasted is what a browser shows, which is not always what a
	// request may carry: the scheme is added where it was left out, and
	// the characters a browser hides are encoded.
	endpoint = normalizeAddress(endpoint)
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return nil, errors.New("接口地址要以 http:// 或 https:// 开头")
	}
	u, _ := url.Parse(endpoint)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", netease.UserAgent)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	// The body is read either way: what a source says when it refuses — a
	// card key that has run out, a song it cannot resolve — is the only
	// thing that tells the listener why nothing plays.
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	ctype := strings.ToLower(res.Header.Get("Content-Type"))
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("接口返回 " + res.Status + saidOf(body))
	}
	// Naming the site instead of its interface is the commonest mistake:
	// the page it answers with says so plainly.
	if looksHTML(body) {
		return nil, errors.New("这个地址返回的是网页，不是接口；要填返回 JSON 的那个地址，例如 …/search?keywords=歌名")
	}
	// A Luoxue (LX) source script is the other thing people fill in
	// here: it is JavaScript, which this app does not run, and is
	// answered with what to do instead.
	if looksLXSource(body) || (isScriptName(u) && !looksJSON(body)) {
		return nil, errors.New(lxSourceHelp)
	}
	// A mirror of NetEase's own API answers with songs that have no
	// address: theirs is asked for below, from the same mirror.
	if songs, ok := neteaseSongs(body); ok {
		return neteaseTracks(endpoint, songs)
	}
	// GD 音乐台 lists songs that name the ids of their stream, not the
	// stream: each is asked for by its id.
	if items, ok := gdItems(body); ok {
		return gdTracks(endpoint, items)
	}
	// What is not data at all is a playlist or a track pasted where an
	// interface was expected: it is read as what it holds, rather than
	// refused over the kind it was filled in under.
	if u != nil && !looksJSON(body) {
		if tracks, err := readLink(u, ctype, body); err == nil {
			return tracks, nil
		}
	}
	listed, err := apiTracks(body)
	if err != nil {
		return nil, err
	}
	var tracks []netease.Track
	for i, t := range listed {
		if track, ok := t.track(i); ok {
			tracks = append(tracks, track)
		}
	}
	if len(tracks) == 0 {
		if why := saidIn(body); why != "" {
			return nil, errors.New("接口没有返回可播放的歌曲：" + why)
		}
		return nil, errors.New("接口没有返回可播放的歌曲")
	}
	return tracks, nil
}

// saidIn is what an answer that lists nothing says about why: the message
// it writes beside its error code, if it writes one.
func saidIn(body []byte) string {
	var said struct {
		Msg     string          `json:"msg"`
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &said) != nil {
		return ""
	}
	if msg := strings.TrimSpace(firstOf(said.Msg, said.Message)); msg != "" {
		return msg
	}
	var err string
	if json.Unmarshal(said.Error, &err) == nil {
		return strings.TrimSpace(err)
	}
	return ""
}

// saidOf is what a refused answer says, cut down to what belongs in a
// message: the first line, short, and nothing when it says nothing.
func saidOf(body []byte) string {
	said := strings.Join(strings.Fields(string(body)), " ")
	if said == "" {
		return ""
	}
	if r := []rune(said); len(r) > 200 {
		said = string(r[:200]) + "…"
	}
	return "：" + said
}

// apiTrack is a track as an HTTP source writes one. The same thing is
// written differently by different interfaces — Meting writes a song as
// "title", "author" and "url", a hand-rolled endpoint as "song",
// "singer" and "src" — so every name it is written under is read.
type apiTrack struct {
	Name     string  `json:"name"`
	Title    string  `json:"title"`
	Song     string  `json:"song"`
	Artists  names   `json:"artists"`
	Artist   names   `json:"artist"`
	Author   names   `json:"author"`
	Singer   names   `json:"singer"`
	Album    string  `json:"album"`
	Duration float64 `json:"duration"`
	DT       float64 `json:"dt"`
	Interval float64 `json:"interval"`
	URL      string  `json:"url"`
	PlayURL  string  `json:"playUrl"`
	Play     string  `json:"play_url"`
	Src      string  `json:"src"`
	Cover    string  `json:"cover"`
	PicURL   string  `json:"picUrl"`
	Pic      string  `json:"pic"`
	Img      string  `json:"img"`
	CoverURL string  `json:"cover_url"`
}

// names reads a field an interface writes either as one name or as a list
// of them: "artist" and "artists" are both written, by different
// interfaces, and both are read here. What is written in a shape this
// does not know is no names, rather than an answer that fails to read.
type names []string

func (n *names) UnmarshalJSON(b []byte) error {
	*n = nil
	b = bytes.TrimSpace(b)
	switch {
	case len(b) == 0, string(b) == "null":
		return nil
	case b[0] == '[':
		var list []string
		if json.Unmarshal(b, &list) != nil {
			// A list of objects, each naming itself.
			var objs []struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(b, &objs) != nil {
				return nil
			}
			for _, o := range objs {
				if name := strings.TrimSpace(o.Name); name != "" {
					list = append(list, name)
				}
			}
		}
		*n = list
		return nil
	default:
		var one string
		if json.Unmarshal(b, &one) != nil {
			return nil
		}
		if one = strings.TrimSpace(one); one != "" {
			*n = names{one}
		}
		return nil
	}
}

// track makes a track of what a source said of one, if it says where it
// plays from.
func (t apiTrack) track(i int) (netease.Track, bool) {
	url := firstOf(t.URL, firstOf(t.PlayURL, firstOf(t.Play, t.Src)))
	if url == "" {
		return netease.Track{}, false
	}
	name := firstOf(t.Name, firstOf(t.Title, t.Song))
	if name == "" {
		name = fmt.Sprintf("第 %d 首", i+1)
	}
	var artists []netease.Artist
	for _, name := range t.artistNames() {
		artists = append(artists, netease.Artist{Name: name})
	}
	return netease.Track{
		ID:      ID(url + "\x00" + name),
		Name:    name,
		Artists: artists,
		Album: netease.Album{
			Name:   t.Album,
			PicURL: firstOf(t.Cover, firstOf(t.PicURL, firstOf(t.Pic, firstOf(t.Img, t.CoverURL)))),
		},
		Duration: durationMs(firstOf2(firstOf2(t.Duration, t.DT), t.Interval)),
		PlayURL:  url,
	}, true
}

// artistNames is who sings a track, as the interface named them: the
// first of the fields it may have written them under, since an interface
// that writes one of them usually writes no other.
func (t apiTrack) artistNames() []string {
	for _, group := range []names{t.Artists, t.Artist, t.Author, t.Singer} {
		var out []string
		for _, name := range group {
			if name = strings.TrimSpace(name); name != "" {
				out = append(out, name)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// durationMs reads how long a track is, in milliseconds: sources write it
// that way, and the ones that write seconds are read as seconds, since no
// song is a second long.
func durationMs(v float64) int64 {
	switch {
	case v <= 0:
		return 0
	case v < 1000:
		return int64(v * 1000)
	default:
		return int64(v)
	}
}

// apiTracks reads the tracks out of what a source answered.
func apiTracks(body []byte) ([]apiTrack, error) {
	var list []apiTrack
	if json.Unmarshal(body, &list) == nil {
		return list, nil
	}
	var held struct {
		Tracks []apiTrack `json:"tracks"`
		Songs  []apiTrack `json:"songs"`
		List   []apiTrack `json:"list"`
		Items  []apiTrack `json:"items"`
		Data   []apiTrack `json:"data"`
		Result struct {
			Tracks []apiTrack `json:"tracks"`
			Songs  []apiTrack `json:"songs"`
			List   []apiTrack `json:"list"`
			Items  []apiTrack `json:"items"`
			Data   []apiTrack `json:"data"`
		} `json:"result"`
	}
	// A field written in another shape than the one read here — "data"
	// holding an object where a list was read — is no reason to give up
	// on an answer: what it does hold is looked for either way, and what
	// went unread is said only when nothing was found at all.
	heldErr := json.Unmarshal(body, &held)
	if got := firstList(held.Tracks, held.Songs, held.List, held.Items, held.Data,
		held.Result.Tracks, held.Result.Songs, held.Result.List, held.Result.Items, held.Result.Data); got != nil {
		return got, nil
	}
	// An answer that holds its list inside an object rather than beside
	// it: {"data":{"list":[…]}} is what several interfaces write.
	var nested struct {
		Data struct {
			Tracks []apiTrack `json:"tracks"`
			Songs  []apiTrack `json:"songs"`
			List   []apiTrack `json:"list"`
			Items  []apiTrack `json:"items"`
		} `json:"data"`
		Result struct {
			Tracks []apiTrack `json:"tracks"`
			Songs  []apiTrack `json:"songs"`
			List   []apiTrack `json:"list"`
			Items  []apiTrack `json:"items"`
		} `json:"result"`
		Playlist struct {
			Tracks []apiTrack `json:"tracks"`
		} `json:"playlist"`
	}
	if json.Unmarshal(body, &nested) == nil {
		if got := firstList(nested.Data.Tracks, nested.Data.Songs, nested.Data.List, nested.Data.Items,
			nested.Result.Tracks, nested.Result.Songs, nested.Result.List, nested.Result.Items,
			nested.Playlist.Tracks); got != nil {
			return got, nil
		}
	}
	if heldErr != nil {
		return nil, errors.New("接口返回的不是能读的 JSON：" + heldErr.Error())
	}
	return nil, nil
}

// firstList is the first of the lists that holds anything: where the
// interfaces read here put the songs of an answer.
func firstList(lists ...[]apiTrack) []apiTrack {
	for _, list := range lists {
		if len(list) > 0 {
			return list
		}
	}
	return nil
}

// firstOf2 returns the first of a and b that is not zero.
func firstOf2(a, b float64) float64 {
	if a != 0 {
		return a
	}
	return b
}
