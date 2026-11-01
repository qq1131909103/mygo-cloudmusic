package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"netease-music-mygo/internal/netease"
)

// gdItem is a song of GD 音乐台, an interface several players share:
// a search lists songs with the ids of their stream and cover, and those
// are asked for one at a time, by id.
type gdItem struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Artist []string `json:"artist"`
	Album  string   `json:"album"`
	PicID  string   `json:"pic_id"`
	URLID  string   `json:"url_id"`
	Source string   `json:"source"`
}

// gdItems reads the songs out of what a GD 音乐台 interface answered, if
// that is what it answered: songs that name the ids their stream is asked
// for by, rather than the stream itself.
func gdItems(body []byte) ([]gdItem, bool) {
	var items []gdItem
	if err := json.Unmarshal(body, &items); err != nil || len(items) == 0 {
		return nil, false
	}
	found := false
	for _, it := range items {
		if it.URLID != "" && it.Name != "" {
			found = true
			break
		}
	}
	if !found {
		return nil, false
	}
	return items, true
}

// gdTracks makes the tracks of songs such an interface listed, asking it
// where each streams from — and, when there are not too many, what it
// looks like.
func gdTracks(endpoint string, items []gdItem) ([]netease.Track, error) {
	base, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return nil, err
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("接口地址要写全，例如 https://music-api.gdstudio.xyz/api.php?types=search&source=netease&name=歌名")
	}
	fallbackSource := base.Query().Get("source")
	if fallbackSource == "" {
		fallbackSource = "netease"
	}

	wanted := make([]int, 0, len(items))
	for i, it := range items {
		if it.URLID != "" && it.Name != "" {
			wanted = append(wanted, i)
		}
	}
	if len(wanted) == 0 {
		return nil, errors.New("接口没有给出可解析的歌曲")
	}

	links := make([]string, len(items))
	covers := make([]string, len(items))
	within(len(wanted), 6, func(k int) {
		i := wanted[k]
		it := items[i]
		source := it.Source
		if source == "" {
			source = fallbackSource
		}
		links[i] = gdAsk(base, map[string]string{"types": "url", "source": source, "id": it.URLID})
		if it.PicID != "" && len(wanted) <= 20 {
			covers[i] = gdAsk(base, map[string]string{"types": "pic", "source": source, "id": it.PicID, "size": "300"})
		}
	})

	var tracks []netease.Track
	for i, it := range items {
		if links[i] == "" {
			continue
		}
		t := netease.Track{
			Name:    it.Name,
			Album:   netease.Album{Name: it.Album, PicURL: covers[i]},
			PlayURL: links[i],
		}
		for _, name := range it.Artist {
			if name = strings.TrimSpace(name); name != "" {
				t.Artists = append(t.Artists, netease.Artist{Name: name})
			}
		}
		t.ID = ID(t.PlayURL + "\x00" + t.Name)
		tracks = append(tracks, t)
	}
	if len(tracks) == 0 {
		return nil, errors.New("接口给的歌曲都拿不到播放地址")
	}
	return tracks, nil
}

// gdAsk asks an interface one thing, by the parameters it names, and
// reads the address it answers with.
func gdAsk(base *url.URL, params map[string]string) string {
	u := *base
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	body, err := getJSON(u.String())
	if err != nil {
		return ""
	}
	var answer struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}
	return answer.URL
}

// within runs fn for each of n, at most workers at a time.
func within(n, workers int, fn func(i int)) {
	if workers < 1 {
		workers = 1
	}
	if workers > n {
		workers = n
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
}
