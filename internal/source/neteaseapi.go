package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"netease-music-mygo/internal/netease"
)

// The shape a NeteaseCloudMusicApi mirror — the one people deploy and open
// in a browser — answers a search or a playlist with: NetEase's own songs,
// which carry an id but no address. The address is asked for separately,
// in one call for the whole list.
type neteaseSong struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Artists  []named `json:"ar"`
	More     []named `json:"artists"`
	Album    albumOf `json:"al"`
	MoreAlbm albumOf `json:"album"`
	Duration int64   `json:"dt"`
	MoreTime int64   `json:"duration"`
}

type named struct {
	Name string `json:"name"`
}

type albumOf struct {
	Name   string `json:"name"`
	PicURL string `json:"picUrl"`
}

// neteaseAnswer is every place those mirrors put the songs of an answer.
type neteaseAnswer struct {
	Result struct {
		Songs  []neteaseSong `json:"songs"`
		Tracks []neteaseSong `json:"tracks"`
	} `json:"result"`
	Songs    []neteaseSong `json:"songs"`
	Playlist struct {
		Tracks []neteaseSong `json:"tracks"`
	} `json:"playlist"`
	Data struct {
		Songs []neteaseSong `json:"songs"`
	} `json:"data"`
}

// neteaseSongs reads the songs out of what a mirror answered, if that is
// what it answered: songs of NetEase's, with an id to ask the address of.
func neteaseSongs(body []byte) ([]neteaseSong, bool) {
	var a neteaseAnswer
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, false
	}
	for _, list := range [][]neteaseSong{a.Result.Songs, a.Result.Tracks, a.Songs, a.Playlist.Tracks, a.Data.Songs} {
		if len(list) > 0 && list[0].ID != 0 {
			return list, true
		}
	}
	return nil, false
}

// neteaseTracks makes the tracks of songs a mirror listed, asking the
// mirror it answered from for where they stream from: the one thing its
// search does not say.
func neteaseTracks(endpoint string, songs []neteaseSong) ([]netease.Track, error) {
	base, err := originOf(endpoint)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, s := range songs {
		if s.ID != 0 {
			ids = append(ids, s.ID)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("接口没有给出歌曲的 id")
	}
	urls, err := neteaseURLs(base, ids)
	if err != nil {
		return nil, err
	}
	var tracks []netease.Track
	for _, s := range songs {
		link := urls[s.ID]
		if link == "" {
			continue
		}
		artists := s.Artists
		if len(artists) == 0 {
			artists = s.More
		}
		album := s.Album
		if album == (albumOf{}) {
			album = s.MoreAlbm
		}
		duration := s.Duration
		if duration == 0 {
			duration = s.MoreTime
		}
		t := netease.Track{
			Name:     s.Name,
			Duration: duration,
			Album:    netease.Album{Name: album.Name, PicURL: album.PicURL},
			PlayURL:  link,
		}
		for _, a := range artists {
			if name := strings.TrimSpace(a.Name); name != "" {
				t.Artists = append(t.Artists, netease.Artist{Name: name})
			}
		}
		t.ID = ID(link + "\x00" + t.Name)
		tracks = append(tracks, t)
	}
	if len(tracks) == 0 {
		return nil, errors.New("接口给的歌曲都拿不到播放地址（可能需要在镜像上登录，或该地区无版权）")
	}
	return tracks, nil
}

// neteaseURLs asks a mirror where the songs of ids stream from.
func neteaseURLs(base string, ids []int64) (map[int64]string, error) {
	list := make([]string, len(ids))
	for i, id := range ids {
		list[i] = strconv.FormatInt(id, 10)
	}
	joined := strings.Join(list, ",")
	// The plain call first; mirrors that answer it empty want the v1 call
	// with a level named, which is what newer ones serve.
	attempts := []string{
		base + "/song/url?id=" + joined,
		base + "/song/url/v1?id=" + joined + "&level=exhigh",
	}
	var lastErr error
	for _, link := range attempts {
		body, err := getJSON(link)
		if err != nil {
			lastErr = err
			continue
		}
		urls := songsURLsOf(body)
		if len(urls) > 0 {
			return urls, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("接口没有回答播放地址")
}

// songsURLsOf reads the addresses out of a /song/url answer.
func songsURLsOf(body []byte) map[int64]string {
	var answer struct {
		Data []struct {
			ID  int64  `json:"id"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil
	}
	urls := make(map[int64]string, len(answer.Data))
	for _, d := range answer.Data {
		if d.URL != "" {
			urls[d.ID] = d.URL
		}
	}
	return urls
}

// originOf is the scheme and host an endpoint answers at: where the rest
// of the mirror's paths hang from.
func originOf(endpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("接口地址要写全，例如 http://127.0.0.1:3000/search")
	}
	return u.Scheme + "://" + u.Host, nil
}

// looksHTML says whether what an endpoint answered is a page rather than
// data: the mistake of naming a site instead of its interface.
func looksHTML(body []byte) bool {
	head := strings.ToLower(strings.TrimSpace(string(body[:min(len(body), 512)])))
	if !strings.HasPrefix(head, "<") {
		return false
	}
	return strings.HasPrefix(head, "<!doctype") || strings.HasPrefix(head, "<html") ||
		strings.Contains(head, "<head") || strings.Contains(head, "<body")
}

// looksJSON says whether what an endpoint answered is data rather than a
// page, a playlist or a track: what an interface answers with starts with
// an object or a list.
func looksJSON(body []byte) bool {
	for _, c := range body {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		}
		return c == '{' || c == '['
	}
	return false
}

// getJSON reads what an endpoint answers, for the calls the app makes of
// its own: a mirror's, alongside the one the listener named.
func getJSON(link string) ([]byte, error) {
	req, err := http.NewRequest("GET", link, nil)
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
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("接口返回 " + res.Status + saidOf(body))
	}
	return body, nil
}
