package app

// Downloading the song playing: a small button beside it saves the song
// to the Downloads folder, at the best a source can give, or at a quality
// the listener picks from the button's menu.
//
// The sources are asked first, for NetEase's songs too: they are what the
// listener keeps to play what NetEase will not serve, and what they
// answer is a whole file where the account's stream may be a preview.
// Without a source, a song is saved from NetEase at the level asked for.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// downloadTimeout is the longest a song may take to come in: a server that
// stalls past it is given up on, so that the app has its one download back.
const downloadTimeout = 10 * time.Minute

// songDownload is one song being saved: where it is saved from, how far
// it is, and what came of it. Its fields are read by the view as the
// download runs, so they are behind its own lock.
type songDownload struct {
	mu    sync.Mutex
	Track netease.Track
	Level string // the level it is saved at, as NetEase names levels
	From  string // the source that answered for it, empty for NetEase's own
	URL   string // where it is saved from
	Path  string // where it is being written
	Done  int64
	Total int64
	Err   string
	// Stage says what the download is doing while it has no bytes to
	// count yet: finding the song on a source can take seconds, and a
	// button that says nothing through them reads as one that did
	// nothing at all.
	Stage string
}

// what returns what the download says it is doing: its stage, while it
// has one, and the share of the song it has otherwise.
func (d *songDownload) what() (stage string, pct float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Total > 0 {
		return "", min(1, float64(d.Done)/float64(d.Total))
	}
	return d.Stage, 0
}

// setStage says what the download is doing, before it has bytes to count.
func (d *songDownload) setStage(stage string) {
	d.mu.Lock()
	d.Stage = stage
	d.mu.Unlock()
}

// progress says how far the download is, 0 to 1, or 0 while the size of
// what is coming is unknown.
func (d *songDownload) progress() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Total <= 0 {
		return 0
	}
	return min(1, float64(d.Done)/float64(d.Total))
}

// state is what the view reads of a download: what it is saving, at which
// level, where to, and how far.
func (d *songDownload) state() (done, total int64, path, level, from string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.Done, d.Total, d.Path, d.Level, d.From
}

// downloadState is the downloads of this run: the one in progress, and
// what the last one did, which the view says in a line under the button.
type downloadState struct {
	mu      sync.Mutex
	current *songDownload
	last    *songDownload
	// dir is where songs are saved; empty means the Downloads folder.
	dir string
}

// startDownload begins saving tr: at the best a source can give when best
// is set, and at the level picked otherwise.
func (a *app) startDownload(tr *netease.Track, best bool, level string) {
	d := a.downloads
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.current != nil {
		// One at a time: the button says what is being saved, and the
		// next press saves the next song.
		d.mu.Unlock()
		a.toast = "还有一首正在下载"
		a.redraw()
		return
	}
	job := &songDownload{Track: *tr, Level: level}
	if best {
		job.Level = source.LevelBest
	}
	d.current, d.last = job, nil
	d.mu.Unlock()
	a.redraw()
	go func() {
		err := a.saveSong(job)
		a.post(func() {
			d.mu.Lock()
			d.current, d.last = nil, job
			d.mu.Unlock()
			if err != nil {
				job.mu.Lock()
				job.Err = err.Error()
				job.mu.Unlock()
				a.toast = "下载失败：" + err.Error()
				return
			}
			_, _, path, _, _ := job.state()
			a.toast = "已下载到 " + shorten(path, 48)
		})
	}()
}

// saving is the download in progress, if any, and the last one that
// finished.
func (a *app) saving() (current, last *songDownload) {
	d := a.downloads
	if d == nil {
		return nil, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current, d.last
}

// savingSays whether a song is being saved, so that the button that saves
// it stays shown in a list while its download runs.
func (a *app) savingSays(tr *netease.Track) bool {
	current, _ := a.saving()
	return current != nil && current.Track.ID == tr.ID
}

// saveSong finds where tr plays at the level asked for and writes it to
// the Downloads folder, under the name the song is known by.
func (a *app) saveSong(job *songDownload) error {
	// Asking a source where the song plays can take seconds, and a script
	// may take many: saying so is what keeps a press from reading as one
	// that did nothing.
	job.setStage("正在找音源…")
	a.redraw()
	url, level, from, err := a.downloadURL(job)
	if err != nil {
		return err
	}
	job.mu.Lock()
	job.URL, job.Level, job.From = url, level, from
	job.mu.Unlock()
	dir, err := a.downloadDir()
	if err != nil {
		return err
	}
	path, err := uniquePath(filepath.Join(dir, fileName(job.Track, url)))
	if err != nil {
		return err
	}
	job.mu.Lock()
	job.Path = path
	job.mu.Unlock()
	job.setStage("")
	a.redraw()
	if err := fetchTo(url, path, func(n, total int64) {
		job.mu.Lock()
		job.Done, job.Total = n, total
		job.mu.Unlock()
		a.redraw()
	}); err != nil {
		return err
	}
	// The cover and the lyrics come with it, as the sidecars this app
	// reads back: a song saved as a bare audio stream would play with no
	// cover and no lyrics at all, which is what a download used to leave
	// behind.
	a.saveSidecars(job, path)
	return nil
}

// saveSidecars writes what the song is beside it: the cover as
// <name>.jpg and the lyrics as <name>.lrc, which is where a folder of
// this machine is read for them. A part that cannot be had is left out:
// a song with no cover still plays.
func (a *app) saveSidecars(job *songDownload, path string) {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	// The cover: the song's own, from wherever it names it.
	if job.Track.Album.PicURL != "" {
		if b, err := source.Bytes(job.Track.Album.PicURL); err == nil && len(b) > 0 {
			ext := ".jpg"
			if !strings.HasPrefix(http.DetectContentType(b), "image/jpeg") {
				ext = coverExt(b)
			}
			os.WriteFile(base+ext, b, 0o644)
		}
	}
	// The lyrics: NetEase's own for its songs, and the .lrc a source of
	// this machine keeps for its own.
	var lrc string
	if job.Track.PlayURL != "" {
		lrc, _ = source.Lyrics(&job.Track)
	} else {
		lrc, _, _, _ = netease.Lyrics(job.Track.ID)
	}
	if s := strings.TrimSpace(lrc); s != "" {
		os.WriteFile(base+".lrc", []byte(lrc), 0o644)
	}
}

// coverExt names a cover's extension by what its bytes say, so that a
// PNG is not saved as a JPEG that no reader opens.
func coverExt(b []byte) string {
	kind := http.DetectContentType(b)
	switch {
	case strings.HasPrefix(kind, "image/png"):
		return ".png"
	case strings.HasPrefix(kind, "image/webp"):
		return ".webp"
	case strings.HasPrefix(kind, "image/gif"):
		return ".gif"
	}
	return ".jpg"
}

// downloadURL is where a song is saved from, and what it is saved as: a
// source the listener keeps answers for it when one can, and NetEase as
// it plays otherwise.
func (a *app) downloadURL(job *songDownload) (url, level, from string, err error) {
	level = job.Level
	entries := a.fallbackSources.list()
	if source.AnswersSongs(entries) {
		found, ferr := source.Fallback(entries, job.Track, level)
		if ferr == nil {
			return found.URL, orLevel(found.Level, level), found.Source, nil
		}
		// A source that cannot find it is no reason to give up on the
		// song: NetEase may still serve it, as it plays.
		last := ferr
		if job.Track.PlayURL != "" {
			return job.Track.PlayURL, level, "", nil
		}
		if info, nerr := netease.SongURL(job.Track.ID, askedLevel(level)); nerr != nil {
			return "", "", "", errors.New("音源和网易云都取不到这首歌：" + last.Error())
		} else {
			return info.URL, info.Level, "", nil
		}
	}
	if job.Track.PlayURL != "" {
		return job.Track.PlayURL, level, "", nil
	}
	info, nerr := netease.SongURL(job.Track.ID, askedLevel(level))
	if nerr != nil {
		return "", "", "", errors.New("网易云取不到这首歌，也没有能替它取流的音源")
	}
	return info.URL, info.Level, "", nil
}

// askedLevel is the level to ask NetEase for: the best a source can give
// is no level of NetEase's, so the best it has is asked for instead.
func askedLevel(level string) string {
	if level == "" || level == source.LevelBest {
		return "hires"
	}
	return level
}

// downloadDir is where songs are saved: the Downloads folder, or one the
// listener set, created if it is not there yet.
func (a *app) downloadDir() (string, error) {
	// What the settings say, and what the app was given, as in tests.
	dir := strings.TrimSpace(a.settings.DownloadDir)
	if a.downloads != nil && strings.TrimSpace(a.downloads.dir) != "" {
		dir = strings.TrimSpace(a.downloads.dir)
	}
	if dir == "" {
		d, err := mygo.App.Path(mygo.PathDownloads)
		if err != nil {
			return "", errors.New("找不到下载文件夹")
		}
		dir = d
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.New("建不了下载文件夹：" + dir)
	}
	return dir, nil
}

// fetchTo downloads url into path, telling how far it is as it goes: what
// came in is counted as it arrives, and a size the server states is the
// total. A part file holds it until it is all in, so that a download that
// fails leaves no half song behind.
func fetchTo(url, path string, progress func(n, total int64)) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", netease.UserAgent)
	// A server that stops sending would otherwise hold the one download the
	// app allows for ever, and every button with it.
	client := &http.Client{Timeout: downloadTimeout}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}
	part := path + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	total := res.ContentLength
	var got int64
	buf := make([]byte, 128<<10)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(part)
				return werr
			}
			got += int64(n)
			if progress != nil {
				progress(got, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(part)
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return err
	}
	if err := os.Rename(part, path); err != nil {
		os.Remove(part)
		return err
	}
	return nil
}

// orLevel is the level to name a download by: what a source said, or the
// one it was asked for when the source did not say.
func orLevel(got, asked string) string {
	if strings.TrimSpace(got) != "" {
		return got
	}
	return asked
}

// fileName is what a song is saved as: the singer and the title, with the
// extension the address it came from has, so that a player opens it.
func fileName(tr netease.Track, url string) string {
	name := strings.TrimSpace(tr.Name)
	if who := strings.TrimSpace(tr.ArtistNames()); who != "" {
		name = who + " - " + name
	}
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '\n', '\r', '\t':
			return '_'
		}
		return r
	}, name)
	if strings.TrimSpace(name) == "" {
		name = "song"
	}
	// Kept short of what a filesystem takes, leaving room for the
	// extension and the number a second download of the same song adds.
	if r := []rune(name); len(r) > 120 {
		name = string(r[:120])
	}
	if strings.Contains(url, ".flac") {
		return name + ".flac"
	}
	return name + ".mp3"
}

// uniquePath numbers a path, "name (2).ext", when a file has its name, as
// a Downloads folder does.
func uniquePath(path string) (string, error) {
	if _, err := os.Lstat(path); err != nil {
		return path, nil
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; i < 1000; i++ {
		p := base + " (" + strconv.Itoa(i) + ")" + ext
		if _, err := os.Lstat(p); err != nil {
			return p, nil
		}
	}
	return "", errors.New("同名文件太多")
}

// downloadButton is the small button beside what plays that saves the
// song: a downward arrow, whose menu asks for the best a source can give
// or for one of the qualities, and says what the last download was.
func (a *app) downloadButton(c *ui.Context, tr *netease.Track, size float32, color ui.Color) ui.Element {
	current, last := a.saving()
	saving := current != nil && current.Track.ID == tr.ID
	// Keyed by the song, not by the button: a list shows one of these on
	// every row, and two elements sharing a key share their state.
	b := iconButton(c.Key("download-"+strconv.FormatInt(tr.ID, 10)), iconDownload, size, color, "下载")
	if saving {
		// The button of the song being saved says so, by the ring around it,
		// so that it keeps the size of the buttons beside it. Every other
		// song's stays live: one song being saved is no reason for the rest
		// to stop answering, and a press on one of them says it is still
		// busy rather than doing nothing at all.
		b.Disabled(true)
		stage, pct := current.what()
		if stage != "" {
			b.Label(stage)
		} else {
			b.Label(fmt.Sprintf("正在下载 %d%%", int(pct*100+0.5)))
		}
		b.Draw(func(p *ui.Painter, r ui.Rect) {
			if pct <= 0 || pct >= 1 {
				return
			}
			// A circle trimmed to how far it is, drawn from the top round
			// clockwise, in the color the icon is: a quarter of the way is a
			// quarter drawn. It is drawn at the size of the icon, and inset from
			// the button's edge, so that it reads as a ring around the arrow
			// rather than as the button's own outline.
			// Under it, the whole circle faintly: without it the part drawn
			// alone would be a tick in the corner, not a ring part filled.
			const ring = 21
			cx, cy := r.X+r.W/2, r.Y+r.H/2
			track := (&ui.Path{}).Circle(cx, cy, ring/2)
			p.StrokePath(track, 2, color.Alpha(0.18))
			done := (&ui.Path{}).Circle(cx, cy, ring/2)
			done.Trim(0, float32(pct))
			p.StrokePath(done, 2, color.Alpha(0.95))
		})
	}
	b.Menu(func(m *ui.Menu) {
		if m.Item("下载最高音质").Chosen() {
			a.startDownload(tr, true, "")
		}
		m.Separator()
		for _, q := range qualityLevels {
			if m.Item("下载" + q.label).Chosen() {
				a.startDownload(tr, false, q.level)
			}
		}
		if done := last; done != nil {
			_, _, _, level, from := done.state()
			line := "上次下载：" + qualityLabel(level)
			if from != "" {
				line += " · " + from
			}
			m.Separator()
			m.Item(line).Disabled(true)
		}
	})
	return b
}

// rowDownloadButton is the arrow on a row of a list: one press saves the
// song at the best a source can give, with no menu in between. A menu
// opened from inside a list does not keep the press — it lands on the row
// under it, which plays that row instead — so the rows ask for the best,
// and the qualities are chosen beside what plays, where the menu works.
func (a *app) rowDownloadButton(c *ui.Context, tr *netease.Track, size float32, color ui.Color) ui.Element {
	current, _ := a.saving()
	saving := current != nil && current.Track.ID == tr.ID
	b := iconButton(c.Key("download-"+strconv.FormatInt(tr.ID, 10)), iconDownload, size, color, "下载")
	if saving {
		stage, pct := current.what()
		b.Disabled(true)
		if stage != "" {
			b.Label(stage)
		} else {
			b.Label(fmt.Sprintf("正在下载 %d%%", int(pct*100+0.5)))
		}
		b.Draw(downloadRing(pct, color))
		return b
	}
	if b.Clicked() {
		a.startDownload(tr, true, "")
	}
	return b
}

// downloadRing draws how far the song being saved has come, as a ring
// around the arrow: the whole circle faintly under the part drawn, so that
// what is drawn reads as a ring part filled rather than as a tick.
func downloadRing(pct float64, color ui.Color) func(*ui.Painter, ui.Rect) {
	return func(p *ui.Painter, r ui.Rect) {
		if pct <= 0 || pct >= 1 {
			return
		}
		const ring = 21
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		track := (&ui.Path{}).Circle(cx, cy, ring/2)
		p.StrokePath(track, 2, color.Alpha(0.18))
		done := (&ui.Path{}).Circle(cx, cy, ring/2)
		done.Trim(0, float32(pct))
		p.StrokePath(done, 2, color.Alpha(0.95))
	}
}

// qualityLabel names a level as the settings do, or the level itself for
// one the settings do not name.
func qualityLabel(level string) string {
	if level == "" || level == source.LevelBest {
		return "最高音质"
	}
	if name := qualityName(level); name != "" {
		return name
	}
	return level
}
