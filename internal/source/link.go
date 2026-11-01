package source

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"netease-music-mygo/internal/netease"
)

// headBytes is how much of a link is read to tell what it holds: enough
// for a playlist, a list, or the tags of a track, and not the track.
const headBytes = 256 << 10

// lxSourceHelp is what is said to a listener who filled in a Luoxue (LX)
// audio-source script: JavaScript this app does not run. It names the way
// such a source is used here — the bridge turns it into the interface
// address that is filled in instead — rather than answering that the
// link is not a playlist, which tells them nothing to do.
const lxSourceHelp = "这是一份洛雪（LX）音源脚本，程序本身不运行 JS；" +
	"用配套的音源桥把它转成接口地址，再填那个接口地址（见 outputs/hyw-音源桥/README.md）"

// looksLXSource says whether what an address answered with is a Luoxue
// (LX) audio-source script. Such a script declares itself in what it
// reads out of the sandbox it runs in, which no list, playlist or
// interface does, so naming those few things tells it apart from them.
func looksLXSource(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	head := latin1(body[:min(len(body), 8<<10)])
	return strings.Contains(head, "globalThis.lx") ||
		strings.Contains(head, "lx.on(") ||
		strings.Contains(head, "EVENT_NAMES") && strings.Contains(head, "musicUrl")
}

// isScriptName says whether an address names a script by its extension:
// the last thing tried, for a script that says nothing about itself.
func isScriptName(u *url.URL) bool {
	if u == nil {
		return false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".js", ".mjs", ".cjs":
		return true
	}
	return false
}

// Link lists what a link holds: a track, a playlist the m3u, m3u8 or pls
// format writes, or an endpoint that answers with JSON, as an HTTP source
// does. Which it is comes from what the link answers with, and from its
// name when the answer does not say.
//
// Only the start of the link is read (a range request), so that listing a
// two-hour set costs no more than listing a single track.
func Link(link string) ([]netease.Track, error) {
	if strings.TrimSpace(link) == "" {
		return nil, errors.New("还没有填写链接")
	}
	// A link is pasted as a browser shows it: without its scheme, with
	// the characters a browser hides.
	link = normalizeAddress(link)
	u, err := url.Parse(link)
	if err != nil {
		return nil, errors.New("这个链接读不懂：" + err.Error())
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("链接要以 http:// 或 https:// 开头")
	}
	if u.Host == "" {
		return nil, errors.New("链接里没有站名")
	}
	body, ctype, err := fetchHead(u)
	if err != nil {
		return nil, err
	}
	// A script is not a link this app reads, and is answered as what it
	// is before being read as anything else.
	if looksLXSource(body) {
		return nil, errors.New(lxSourceHelp)
	}
	// A list is read whole: half a JSON document parses as nothing, and
	// lists grow with the playlist. Audio and playlists keep the head.
	if strings.Contains(ctype, "json") {
		if whole, err := getJSON(u.String()); err == nil {
			body = whole
		}
	}
	// A link to a mirror of NetEase's own API holds songs without an
	// address; theirs is asked for, as an endpoint's is.
	if songs, ok := neteaseSongs(body); ok {
		return neteaseTracks(u.String(), songs)
	}
	if items, ok := gdItems(body); ok {
		return gdTracks(u.String(), items)
	}
	if looksHTML(body) {
		return nil, errors.New("这个链接返回的是网页，不是音频或播放列表")
	}
	return readLink(u, ctype, body)
}

// fetchHead reads the start of the link, and what it says it is.
func fetchHead(u *url.URL) (body []byte, ctype string, err error) {
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", netease.UserAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", headBytes-1))
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	// A server that ignores the range answers with the whole file; only
	// the start of it is kept either way.
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		said, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return nil, "", errors.New("链接返回 " + res.Status + saidOf(said))
	}
	body, err = io.ReadAll(io.LimitReader(res.Body, headBytes))
	if err != nil {
		return nil, "", err
	}
	return body, strings.ToLower(res.Header.Get("Content-Type")), nil
}

// readLink reads what the head of the link holds, in the format it names,
// or in the one its content is written in when it names none.
func readLink(u *url.URL, ctype string, body []byte) ([]netease.Track, error) {
	ext := strings.ToLower(path.Ext(u.Path))
	switch {
	case strings.Contains(ctype, "json") || ext == ".json":
		return jsonTracks(body)
	case isM3U(ctype, ext, body):
		return m3uTracks(u, body)
	case isPLS(ctype, ext, body):
		return plsTracks(u, body)
	case strings.HasPrefix(ctype, "audio/") || isAudioExt(ext) || isAudio(body):
		return []netease.Track{headTrack(u, body)}, nil
	}
	// A link that says nothing about itself is read by what it holds.
	if tracks, err := jsonTracks(body); err == nil {
		return tracks, nil
	}
	if isM3U("", "", body) {
		return m3uTracks(u, body)
	}
	if isPLS("", "", body) {
		return plsTracks(u, body)
	}
	if isAudio(body) {
		return []netease.Track{headTrack(u, body)}, nil
	}
	if isScriptName(u) {
		return nil, errors.New(lxSourceHelp)
	}
	return nil, errors.New("这个链接不是音频，也不是播放列表或接口")
}

func isM3U(ctype, ext string, body []byte) bool {
	return strings.Contains(ctype, "mpegurl") || ext == ".m3u" || ext == ".m3u8" ||
		strings.HasPrefix(strings.TrimSpace(string(latin1(body[:min(len(body), 16)]))), "#EXTM3U")
}

func isPLS(ctype, ext string, body []byte) bool {
	return strings.Contains(ctype, "pls") || ext == ".pls" ||
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(latin1(body[:min(len(body), 16)])))), "[playlist]")
}

func isAudioExt(ext string) bool {
	switch ext {
	case ".mp3", ".flac", ".m4a", ".aac", ".ogg", ".opus", ".wav", ".wma", ".ape", ".alac":
		return true
	}
	return false
}

func isAudio(body []byte) bool {
	switch {
	case len(body) < 4:
		return false
	case string(body[:3]) == "ID3":
		return true
	case string(body[:4]) == "fLaC":
		return true
	case string(body[:4]) == "OggS":
		return true
	case string(body[:4]) == "RIFF", string(body[:4]) == "FORM":
		return true
	case len(body) >= 8 && string(body[4:8]) == "ftyp":
		return true
	}
	return false
}

// jsonTracks reads a list out of what an endpoint answered.
func jsonTracks(body []byte) ([]netease.Track, error) {
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
			return nil, errors.New("链接里的列表没有可播放的歌曲：" + why)
		}
		return nil, errors.New("链接里的列表没有可播放的歌曲")
	}
	return tracks, nil
}

// m3uTracks reads a playlist in the m3u or m3u8 format: the addresses it
// lists, each with the name and length its #EXTINF line gives it.
func m3uTracks(base *url.URL, body []byte) ([]netease.Track, error) {
	var tracks []netease.Track
	var title string
	var ms int64
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if rest, ok := strings.CutPrefix(line, "#EXTINF:"); ok {
				length, name := rest, ""
				if i := strings.Index(rest, ","); i >= 0 {
					length, name = rest[:i], strings.TrimSpace(rest[i+1:])
				}
				ms = 0
				if secs, err := strconv.ParseFloat(strings.TrimSpace(length), 64); err == nil && secs > 0 {
					ms = int64(secs * 1000)
				}
				title = name
			}
			continue
		}
		ref, err := base.Parse(line)
		if err != nil {
			continue
		}
		tracks = append(tracks, linkTrack(ref, title, ms))
		title, ms = "", 0
	}
	if len(tracks) == 0 {
		return nil, errors.New("播放列表里没有能播的地址")
	}
	return tracks, nil
}

// plsTracks reads a playlist in the pls format: FileN names the address,
// TitleN its name and LengthN its length in seconds.
func plsTracks(base *url.URL, body []byte) ([]netease.Track, error) {
	files := map[int]string{}
	titles := map[int]string{}
	lengths := map[int]int64{}
	order := []int{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		lower := strings.ToLower(key)
		switch {
		case strings.HasPrefix(lower, "file"):
			if n, err := strconv.Atoi(key[4:]); err == nil {
				files[n], order = value, append(order, n)
			}
		case strings.HasPrefix(lower, "title"):
			if n, err := strconv.Atoi(key[5:]); err == nil {
				titles[n] = value
			}
		case strings.HasPrefix(lower, "length"):
			if n, err := strconv.Atoi(key[6:]); err == nil {
				if secs, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && secs > 0 {
					lengths[n] = secs * 1000
				}
			}
		}
	}
	var tracks []netease.Track
	for _, n := range order {
		ref, err := base.Parse(strings.TrimSpace(files[n]))
		if err != nil {
			continue
		}
		tracks = append(tracks, linkTrack(ref, titles[n], lengths[n]))
	}
	if len(tracks) == 0 {
		return nil, errors.New("播放列表里没有能播的地址")
	}
	return tracks, nil
}

// linkTrack makes the track a playlist lists: its address, and the name
// and length the playlist tells of it, if it tells any.
func linkTrack(u *url.URL, title string, ms int64) netease.Track {
	name := strings.TrimSpace(title)
	artist := ""
	// A playlist usually names a track "Artist - Name".
	if i := strings.Index(name, " - "); i > 0 {
		artist, name = strings.TrimSpace(name[:i]), strings.TrimSpace(name[i+3:])
	}
	if name == "" {
		name = nameOf(u)
	}
	tr := netease.Track{Name: name, Duration: ms, PlayURL: u.String()}
	if artist != "" {
		tr.Artists = []netease.Artist{{Name: artist}}
	}
	return tr
}

// headTrack makes the track a link to an audio file names, with what its
// tags say of it: enough to list it, not the whole file.
func headTrack(u *url.URL, body []byte) netease.Track {
	tr := netease.Track{Name: nameOf(u), PlayURL: u.String()}
	tags := id3Text(body)
	if v := tags["TIT2"]; v != "" {
		tr.Name = v
	}
	if v := tags["TPE1"]; v != "" {
		for _, name := range strings.Split(v, "/") {
			if name = strings.TrimSpace(name); name != "" {
				tr.Artists = append(tr.Artists, netease.Artist{Name: name})
			}
		}
	}
	if v := tags["TALB"]; v != "" {
		tr.Album.Name = v
	}
	if v := tags["TLEN"]; v != "" {
		if ms, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && ms > 0 {
			tr.Duration = ms
		}
	}
	return tr
}

// nameOf names a track after the address it plays from: its file name
// without the extension, or the station it comes from.
func nameOf(u *url.URL) string {
	base := path.Base(u.Path)
	if base == "." || base == "/" || base == "" {
		return u.Host
	}
	if ext := path.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if unescaped, err := url.PathUnescape(base); err == nil {
		base = unescaped
	}
	return base
}

// id3Text reads the text frames of the ID3v2 header at the start of body:
// what names a track, and how long it is. Only the frames the head holds
// are read; a track whose tags come after more than that is named after
// its address instead.
func id3Text(body []byte) map[string]string {
	tags := map[string]string{}
	if len(body) < 10 || string(body[:3]) != "ID3" {
		return tags
	}
	version := body[3]
	end := 10 + synchsafe(body[6:10])
	if end > len(body) {
		end = len(body)
	}
	at := 10
	if body[5]&0x40 != 0 && at+4 <= end {
		at += 4 + synchsafe(body[at:at+4])
	}
	for at+10 <= end {
		id := string(body[at : at+4])
		if id[0] == 0 {
			break
		}
		size := int(binary.BigEndian.Uint32(body[at+4 : at+8]))
		if version == 4 {
			size = synchsafe(body[at+4 : at+8])
		}
		start := at + 10
		if size <= 0 || start+size > end {
			break
		}
		if id[0] == 'T' && id != "TXXX" && id != "TXX" {
			if v := decodeText(body[start : start+size]); v != "" {
				tags[id] = v
			}
		}
		at = start + size
	}
	return tags
}
