package app

// The page of the music on this machine: a page of its own, beside the
// ones that show NetEase's, because a listener who keeps their own music
// goes looking for it there rather than under the third-party sources.
//
// It lists the folders the settings keep, and the music in them. Adding
// one is what the page is for: the button that does it is on the page, in
// the middle while there is nothing yet, and in the header once there is.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// localEntries are the sources that play from this machine: folders of
// files, which are also sources of the third-party kind the settings
// keep. A page of its own over the same list, so that a folder added here
// is one the settings show too, and the other way round.
func (a *app) localEntries() []source.Entry {
	var kept []source.Entry
	for _, e := range a.settings.Sources {
		if e.Kind == source.KindFolder && source.Label(e.Kind) != "" {
			kept = append(kept, e)
		}
	}
	return kept
}

// localTracks are the songs the folders of this machine hold.
func (a *app) localTracks() []netease.Track { return a.localSongs }

// loadLocal lists what the folders of this machine hold, on their own: a
// folder is read in milliseconds, and the page of this machine's music
// should not wait for an endpoint that may take seconds to answer.
func (a *app) loadLocal(force bool) {
	entries := a.localEntries()
	key := sourceKey(entries)
	if !force && key == a.localFor {
		return
	}
	a.localFor = key
	a.localGen++
	gen := a.localGen
	a.localLoading, a.localFailed = true, nil
	if len(entries) == 0 {
		a.localLoading, a.localSongs = false, nil
		return
	}
	go func() {
		var songs []netease.Track
		var failed []string
		for _, e := range entries {
			listed, err := listOne(e)
			if err != nil {
				failed = append(failed, e.Title()+"："+err.Error())
				continue
			}
			for _, t := range listed {
				t.Source = e.Title()
				songs = append(songs, t)
			}
		}
		a.post(func() {
			if gen != a.localGen {
				return
			}
			a.localLoading, a.localSongs, a.localFailed = false, songs, failed
			a.redraw()
		})
	}()
}

// localPage lists the music of this machine, or offers to add a folder of
// it: the page a listener with their own music comes to.
func (a *app) localPage(c *ui.Context) {
	kept := a.localEntries()
	songs := a.localTracks()
	if len(kept) == 0 {
		a.localEmpty(c)
		return
	}
	a.trackRows(c, songs, true, a.localHeading, func(c *ui.Context) {
		a.localRows(c, kept)
	})
}

// localHeading names the page after the folders it lists, with the
// buttons that list them again, add another, and change where they are.
func (a *app) localHeading(c *ui.Context) {
	sub := localSummary(a.localEntries(), len(a.localTracks()))
	p := a.pal
	ui.Row(c).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).Children(func() { a.heading(c, "本地音乐", sub) })
		ui.Row(c).Gap(4).Margin(0, 0, 24, 0).Children(func() {
			if iconButton(c.Key("local-refresh"), iconReload, 17, p.muted, "重新读取").Clicked() {
				a.loadLocal(true)
			}
			if ui.Button(c.Key("local-add"), "添加音乐文件夹").Clicked() {
				a.addSourceDir()
			}
			if iconButton(c.Key("local-settings"), iconSettings, 17, p.muted, "设置").Clicked() {
				a.openSettings()
			}
		})
	})
}

// localSummary says what the page lists: how many folders, and how many
// songs they hold.
func localSummary(entries []source.Entry, tracks int) string {
	return fmt.Sprintf("%d 个文件夹 · %d 首", len(entries), tracks)
}

// localQuery is what the page's own field is filtering by: the songs of
// this machine are searched where they are, which is what a listener with
// a folder of their own music looks through.
func (a *app) localShown() []netease.Track {
	songs := a.localTracks()
	q := strings.TrimSpace(a.localSearch)
	if q == "" {
		return songs
	}
	return filterTracks(songs, q)
}

// localToolbar is the row above the songs of this machine: the field that
// searches them by title, singer, album or folder.
func (a *app) localToolbar(c *ui.Context) {
	p := a.pal
	if len(a.localTracks()) == 0 {
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 0, 10, 0).Children(func() {
		field := ui.Row(c).Height(32).PaddingX(10).Gap(7).Radius(9).AlignItems(ui.Center).Grow(1).Basis(0).MinWidth(0)
		field.Background(p.hover).Border(1, p.hairline)
		field.Children(func() {
			ui.Icon(c, iconSearch).Size(14, 14).TextColor(p.muted)
			ui.TextInput(c.Key("local-search"), &a.localSearch).
				Placeholder("搜索这台电脑上的音乐").FontSize(13).Label("搜索本地音乐").Grow(1).MinWidth(0)
		})
		if q := strings.TrimSpace(a.localSearch); q != "" {
			ui.Textf(c, "找到 %d 首", len(a.localShown())).FontSize(12).TextColor(p.muted)
			if iconButton(c.Key("local-search-clear"), iconX, 14, p.muted, "清除搜索").Clicked() {
				a.localSearch = ""
			}
		}
	})
}

// localSidecars is the button that fetches what the songs of this machine
// are missing: the cover and the lyrics beside a file that was saved as a
// bare audio stream. A song that has them already is left alone.
func (a *app) localSidecars(c *ui.Context) {
	p := a.pal
	songs := a.localTracks()
	missing := 0
	for i := range songs {
		if songNeedsSidecars(&songs[i]) {
			missing++
		}
	}
	if missing == 0 {
		return
	}
	label := fmt.Sprintf("补齐封面和歌词（%d 首）", missing)
	if a.sidecarBusy {
		label = "正在补齐…"
	}
	if ui.Button(c.Key("local-sidecars"), label).Disabled(a.sidecarBusy).Clicked() {
		a.fetchSidecars()
	}
	ui.Text(c, "下载时没带封面歌词的音乐，可以在这里补上").FontSize(11.5).TextColor(p.muted).Padding(0, 8, 0, 0)
}

// songNeedsSidecars says whether a song of this machine is missing the
// cover or the lyrics that would sit beside it.
func songNeedsSidecars(tr *netease.Track) bool {
	path, ok := source.LocalPath(tr.PlayURL)
	if !ok {
		return false
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	hasCover := false
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		if _, err := os.Stat(base + ext); err == nil {
			hasCover = true
			break
		}
	}
	if tr.Album.PicURL != "" && strings.HasPrefix(tr.Album.PicURL, "art://") {
		hasCover = true // the art is inside the file already
	}
	if !hasCover {
		return true
	}
	if _, err := os.Stat(base + ".lrc"); err != nil {
		return true
	}
	return false
}

// fetchSidecars reads the cover and the lyrics of every song of this
// machine that is missing them, and writes them beside the files.
func (a *app) fetchSidecars() {
	if a.sidecarBusy {
		return
	}
	songs := a.localTracks()
	a.sidecarBusy = true
	go func() {
		done, failed := 0, 0
		for i := range songs {
			if !songNeedsSidecars(&songs[i]) {
				continue
			}
			if err := a.writeSidecars(&songs[i]); err != nil {
				failed++
				continue
			}
			done++
		}
		a.post(func() {
			a.sidecarBusy = false
			switch {
			case failed > 0:
				a.toast = fmt.Sprintf("补齐了 %d 首，%d 首没找到", done, failed)
			default:
				a.toast = fmt.Sprintf("已补齐 %d 首的封面和歌词", done)
			}
			a.loadLocal(true)
		})
	}()
}

// writeSidecars writes the cover and the lyrics of one song of this
// machine beside its file, where a folder is read for them.
func (a *app) writeSidecars(tr *netease.Track) error {
	path, ok := source.LocalPath(tr.PlayURL)
	if !ok {
		return errors.New("不是本机文件")
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	wrote := false

	// A file of this machine names no song of NetEase's: its id is one the
	// app made up from its path, which no account knows. Whatever it does
	// not have — a cover of its own, lyrics — is looked for in the song it
	// is, by the name and the singer it was read as.
	hasCover := strings.HasPrefix(tr.Album.PicURL, "art://")
	pic := tr.Album.PicURL
	if hasCover {
		// The art inside the file is the cover: it is not written out.
		pic = ""
	}
	needsLyrics := false
	if _, err := os.Stat(base + ".lrc"); err != nil {
		needsLyrics = true
	}

	known := tr
	if tr.ID < 0 && (pic == "" || needsLyrics) {
		if match := a.findSong(tr); match != nil {
			known = match
			if pic == "" {
				pic = match.Album.PicURL
			}
		}
	}

	if pic != "" && !strings.HasPrefix(pic, "art://") {
		if b, err := source.Bytes(pic); err == nil && len(b) > 0 {
			if os.WriteFile(base+coverExt(b), b, 0o644) == nil {
				wrote = true
			}
		}
	}
	if needsLyrics && known.ID > 0 {
		if lrc, _, _, err := netease.Lyrics(known.ID); err == nil && strings.TrimSpace(lrc) != "" {
			if os.WriteFile(base+".lrc", []byte(lrc), 0o644) == nil {
				wrote = true
			}
		}
	}
	if !wrote {
		return errors.New("没有可用的封面或歌词")
	}
	return nil
}

// searchSongs finds the songs NetEase knows by a query; tests replace it.
var searchSongs = netease.Search

// foundSongs keeps what a file of this machine was found to be, by the
// path it is at: a song plays again without asking NetEase about it again.
var foundSongs sync.Map // string -> foundSong

// foundSong is what a file of this machine was found to be: the song of
// NetEase's it is, and the cover and the lyrics fetched for it. A file
// that names no song keeps both empty.
type foundSong struct {
	cover []byte
	lrc   string
}

// localMeta returns the cover and the lyrics of a file of this machine
// that has neither beside it, for the player to show while it plays. What
// it finds is written beside the file as well, so that the page reads it
// back with the rest.
func (a *app) localMeta(tr *netease.Track) ([]byte, string) {
	path, ok := source.LocalPath(tr.PlayURL)
	if !ok {
		return nil, ""
	}
	if kept, hit := foundSongs.Load(path); hit {
		got := kept.(foundSong)
		a.showLocalCover(tr, path, got.cover)
		return got.cover, got.lrc
	}
	match := a.findSong(tr)
	var got foundSong
	if match != nil {
		if b, err := source.Bytes(netease.Sized(match.Album.PicURL, 600)); err == nil {
			got.cover = b
		}
		if lrc, _, _, err := netease.Lyrics(match.ID); err == nil {
			got.lrc = strings.TrimSpace(lrc)
		}
	}
	foundSongs.Store(path, got)
	a.showLocalCover(tr, path, got.cover)
	return got.cover, got.lrc
}

// showLocalCover puts the cover found for a file of this machine where the
// views read it: a track's cover is the address it carries, so the one
// fetched is written where an address can name it, and the track playing
// takes that address. Without it, a file whose cover NetEase has showed
// none at all — the player bar, the lists and Now Playing all read the
// track's own address, never the bytes the player was handed.
func (a *app) showLocalCover(tr *netease.Track, path string, cover []byte) {
	if len(cover) == 0 || tr.Album.PicURL != "" {
		return
	}
	written := filepath.Join(coverDir(), source.CachedCoverName(path, coverExt(cover)))
	if err := os.MkdirAll(filepath.Dir(written), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(written, cover, 0o644); err != nil {
		return
	}
	apply := func() {
		tr.Album.PicURL = written
		// The lists read the tracks the sources listed, and the player bar
		// the one playing: both take the cover from the address.
		for i := range a.sourceTracks {
			if a.sourceTracks[i].PlayURL == path {
				a.sourceTracks[i].Album.PicURL = written
			}
		}
		if cur := a.pl.Current(); cur != nil && cur.PlayURL == path {
			cur.Album.PicURL = written
		}
		a.redraw()
	}
	if a.win == nil {
		// As in tests, where there is no thread to post to.
		apply()
		return
	}
	a.post(apply)
}

// coverDir is where covers found for files of this machine are kept: the
// cache folder of the app, which the system may clear.
func coverDir() string {
	if dir := coverDirName(); dir != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), "mygo-local-covers")
}

// coverDirName is the cache folder, or "" when the app cannot name one.
func coverDirName() string {
	dir, err := mygo.App.Path(mygo.PathCache)
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "local-covers")
}

// findSong returns the song of NetEase's that a file of this machine is,
// by the name and the singer the file was read as: what its cover and its
// lyrics are fetched from, since the file itself names no song of the
// account's. A search answers with other songs too, covers among them, so
// only one of that very name counts: a file that finds none is left with
// what it has, rather than given another song's cover.
func (a *app) findSong(tr *netease.Track) *netease.Track {
	name := strings.TrimSpace(tr.Name)
	if name == "" {
		return nil
	}
	// A file's name often carries what the song is not titled by — the
	// version, a show it was sung on — so the name is asked as it is, and
	// then without that part.
	for _, want := range titleForms(name) {
		if got := a.askNetease(want, tr.ArtistNames()); got != nil {
			return got
		}
	}
	return nil
}

// askNetease asks NetEase for a title and singer, and returns the song of
// that very title: a search answers with covers of it too, which name
// another singer, and with songs whose titles merely begin alike.
func (a *app) askNetease(title, artists string) *netease.Track {
	found, err := searchSongs(strings.TrimSpace(title+" "+artists), 8)
	if err != nil || len(found) == 0 {
		return nil
	}
	want := strings.ToLower(strings.TrimSpace(title))
	who := strings.ToLower(strings.TrimSpace(artists))
	var byName *netease.Track
	for i := range found {
		if strings.ToLower(strings.TrimSpace(found[i].Name)) != want {
			continue
		}
		if who == "" || strings.Contains(strings.ToLower(found[i].ArtistNames()), who) {
			return &found[i]
		}
		if byName == nil {
			byName = &found[i]
		}
	}
	return byName
}

// titleForms are the names to ask NetEase for, most exact first: the name
// as it is, and then without the bracketed part a download appends — a
// remix, a live take, the album it came from.
func titleForms(name string) []string {
	out := []string{name}
	base := name
	for _, pair := range [][2]string{{"(", ")"}, {"（", "）"}, {"[", "]"}, {"【", "】"}} {
		if i := strings.Index(base, pair[0]); i > 0 {
			base = strings.TrimSpace(base[:i])
		}
	}
	base = strings.TrimSpace(strings.TrimRight(base, "-_ "))
	if base != "" && !strings.EqualFold(base, name) {
		out = append(out, base)
	}
	return out
}

// localRows lists the folders themselves under the music in them: one row
// each, saying where it is and what it holds.
func (a *app) localRows(c *ui.Context, kept []source.Entry) {
	p := a.pal
	a.section(c, "文件夹")
	ui.Column(c).Gap(0).Children(func() {
		for i := range kept {
			e := &kept[i]
			row := ui.Row(c).Key("local-row-"+e.ID).Padding(10, 4).Gap(12).
				AlignItems(ui.Center).MinHeight(44)
			row.Children(func() {
				ui.Box(c).Size(22, 22).Center().Children(func() {
					ui.Icon(c, iconFolder).Size(15, 15).TextColor(p.muted)
				})
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, e.Title()).FontSize(13.5).FontWeight(500).SingleLine()
					ui.Text(c, shorten(e.Value, 64)).FontSize(12).TextColor(p.muted).SingleLine()
				})
				if ui.Button(c.Key("local-more-"+e.ID), "换一个").Clicked() {
					a.chooseSourceFolder(i)
				}
			})
			if row.Hovered() {
				row.Background(p.hover)
			}
			row.Radius(10)
		}
	})
}

// localEmpty is what the page shows while no folder of this machine is
// kept: what the page is for, and the one button that makes it useful.
func (a *app) localEmpty(c *ui.Context) {
	top, bottom := a.insets(c)
	ui.Column(c).Grow(1).Padding(top, 0, bottom, 0).Center().Gap(14).Children(func() {
		ui.Text(c, "本地音乐").FontSize(22).FontWeight(700)
		ui.Text(c, "听这台电脑上的音乐").TextColor(a.pal.muted).FontSize(13)
		ui.Text(c, "选一个文件夹，里面的音乐就会列出来（mp3、flac、m4a、ogg、wav、wma…）；子文件夹也会一起找").
			TextColor(a.pal.faint).FontSize(12)
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c.Key("local-add-first"), "添加音乐文件夹").Clicked() {
				a.addSourceDir()
			}
			if ui.Button(c.Key("local-open-settings"), "打开设置").Clicked() {
				a.openSettings()
			}
		})
	})
}
