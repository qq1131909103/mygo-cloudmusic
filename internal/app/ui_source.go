package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// listSource lists what the third-party sources hold; tests replace it.
var listSource = source.Load

// listOne lists one source on its own, for the settings to test it with;
// tests replace it too.
var listOne = source.One

// sourceKey names what the settings ask to list, so that the page lists
// again only when that changes.
func sourceKey(entries []source.Entry) string {
	var b strings.Builder
	for _, e := range entries {
		if !e.On() {
			continue
		}
		fmt.Fprintf(&b, "%s\x00%s\x00%s\x00", e.Kind, e.Value, e.Title())
	}
	return b.String()
}

// loadSource lists what the third-party sources of the settings hold: the
// music of their folders, the tracks their endpoints answer with, and what
// their links hold. The sources that are off list nothing, and what has
// been listed is not listed again unless forced to, as a refresh or a
// setting that changed asks.
func (a *app) loadSource(force bool) {
	entries := a.settings.Sources
	key := sourceKey(entries)
	if !force && key == a.sourceFor {
		return
	}
	a.sourceFor = key
	a.sourceGen++
	gen := a.sourceGen
	a.sourceTracks, a.sourceErr, a.sourceFailed = nil, "", nil
	on := 0
	for _, e := range entries {
		if e.On() {
			on++
		}
	}
	if on == 0 {
		a.sourceLoading = false
		return
	}
	a.sourceLoading = true
	go func() {
		// Each source is asked at once, and what it answers is shown as it
		// comes: a folder is read in milliseconds while an endpoint may
		// take seconds, and the page should not stand empty for the slower.
		var asked []int
		for i, e := range entries {
			if e.On() {
				asked = append(asked, i)
			}
		}
		answers := make(chan sourceAnswer, len(asked))
		for _, i := range asked {
			go func(at int, e source.Entry) {
				listed, err := listOne(e)
				got := sourceAnswer{at: at, holds: listed}
				if err != nil {
					got.why = e.Title() + "：" + err.Error()
				} else {
					for k := range got.holds {
						got.holds[k].Source = e.Title()
					}
				}
				answers <- got
			}(i, entries[i])
		}
		landed := make(map[int]sourceAnswer, len(asked))
		for range asked {
			got := <-answers
			landed[got.at] = got
			shown, failed := gather(asked, landed)
			a.post(func() {
				if gen == a.sourceGen {
					a.sourceTracks, a.sourceFailed = shown, failed
					a.redraw()
				}
			})
		}
		tracks, failed := gather(asked, landed)
		a.post(func() { a.sourceLanded(gen, tracks, failed) })
	}()
}

// sourceAnswer is what one source answered when it was listed, and why it
// could not be, if it could not.
type sourceAnswer struct {
	at    int
	holds []netease.Track
	why   string
}

// gather puts what the sources answered in the order they are kept, each
// track once: the ones still being asked hold nothing yet, and are left
// out until they answer.
func gather(order []int, landed map[int]sourceAnswer) ([]netease.Track, []string) {
	var tracks []netease.Track
	var failed []string
	seen := map[string]bool{}
	for _, at := range order {
		got, ok := landed[at]
		if !ok {
			continue
		}
		if got.why != "" {
			failed = append(failed, got.why)
			continue
		}
		for _, t := range got.holds {
			if t.PlayURL != "" {
				if seen[t.PlayURL] {
					continue
				}
				seen[t.PlayURL] = true
			}
			tracks = append(tracks, t)
		}
	}
	return tracks, failed
}

// sourceLanded takes what listing gen found, unless other sources were
// asked for meanwhile.
func (a *app) sourceLanded(gen int, tracks []netease.Track, failed []string) {
	if gen != a.sourceGen {
		return
	}
	a.sourceLoading = false
	a.sourceTracks = tracks
	a.sourceFailed = failed
	a.sourceErr = ""
	if len(failed) > 0 && len(tracks) == 0 {
		// Nothing at all could be listed: the page says why, instead of
		// standing empty.
		a.sourceErr = strings.Join(failed, "\n")
	}
}

// reloadSource lists the sources again when the settings name others, or
// none.
func (a *app) reloadSource() { a.loadSource(false) }

// postSettings runs fn on the settings window's thread and draws that
// window again. Without it, as in tests, it drops fn.
func (a *app) postSettings(fn func()) {
	win := a.settingsWin
	if win == nil {
		return
	}
	win.Update(func() {
		fn()
		win.Invalidate()
	})
}

// testSource lists the source at i on its own and keeps what it found,
// so that the settings can say whether an address works the moment it
// is filled in: the shortest way to tell a typo from an interface that
// answers with nothing.
func (a *app) testSource(i int) {
	if i < 0 || i >= len(a.settings.Sources) {
		return
	}
	e := a.settings.Sources[i]
	entry := source.Entry{
		ID:      e.ID,
		Name:    e.Title(),
		Kind:    e.Kind,
		Value:   strings.TrimSpace(e.Value),
		Enabled: true,
	}
	a.setSourceBusy(e.ID, true)
	go func() {
		// A script lists nothing, so it is asked what it can do instead:
		// run it once, and say which platforms it resolves. That is also
		// what tells a source script from a file that merely ends in .js.
		if entry.Kind == source.KindScript {
			said, err := source.ScriptInfo(entry.Value)
			if err != nil {
				said = "用不了：" + err.Error()
			}
			a.postSettings(func() {
				a.setSourceBusy(e.ID, false)
				a.setSourceTest(entry, said)
			})
			return
		}
		tracks, err := listOne(entry)
		said := sourceTestLine(tracks, err)
		a.postSettings(func() {
			a.setSourceBusy(e.ID, false)
			a.setSourceTest(entry, said)
		})
	}()
}

// sourceTestLine says, in one line, what testing a source found: how many
// tracks it lists and the first of them, or why it lists none.
func sourceTestLine(tracks []netease.Track, err error) string {
	switch {
	case err != nil:
		return "用不了：" + err.Error()
	case len(tracks) == 0:
		return "地址能打开，但没有读出可播放的歌曲"
	}
	shown := make([]string, 0, 2)
	for i, tr := range tracks {
		if i == 2 {
			break
		}
		shown = append(shown, trackLineOf(tr))
	}
	return fmt.Sprintf("可以，读到 %d 首：%s", len(tracks), strings.Join(shown, "；"))
}

// trackLineOf names a track in one line: what it is called, and who sings
// it, where the source said.
func trackLineOf(tr netease.Track) string {
	var artists []string
	for _, a := range tr.Artists {
		if name := strings.TrimSpace(a.Name); name != "" {
			artists = append(artists, name)
		}
	}
	if len(artists) == 0 {
		return tr.Name
	}
	return tr.Name + " — " + strings.Join(artists, "/")
}

// sourceTestKey names what a test was run for: the entry, and the address
// it had then, so that an address that has changed is not shown the
// result of the one before it.
func sourceTestKey(e source.Entry) string {
	return e.ID + "\x00" + e.Kind + "\x00" + strings.TrimSpace(e.Value)
}

// setSourceTest keeps what testing a source found.
func (a *app) setSourceTest(e source.Entry, said string) {
	if a.sourceTests == nil {
		a.sourceTests = make(map[string]string)
	}
	a.sourceTests[sourceTestKey(e)] = said
}

// setSourceBusy says whether a test of a source is running.
func (a *app) setSourceBusy(id string, busy bool) {
	if a.sourceBusy == nil {
		a.sourceBusy = make(map[string]bool)
	}
	if busy {
		a.sourceBusy[id] = true
		return
	}
	delete(a.sourceBusy, id)
}

// sourceTestNote is what the settings say beside a source's address: the
// result of its last test, while one is running, and what an address of
// that kind looks like before any.
func (a *app) sourceTestNote(e source.Entry) string {
	if a.sourceBusy[e.ID] {
		return "正在测试…"
	}
	if said := a.sourceTests[sourceTestKey(e)]; said != "" {
		return said
	}
	return sourceKindHint(e.Kind)
}

// sourceKindHint says what an address of a kind looks like, for a source
// that has not been tested yet.
func sourceKindHint(kind string) string {
	switch kind {
	case source.KindAPI:
		return "浏览器能打开、返回 JSON 的地址，不带 http:// 也认得。地址里写 {keyword} 就按歌名搜（见下），不写就是一张歌单"
	case source.KindLink:
		return "m3u 播放列表、单曲直链，或一个接口地址；写 {keyword} 同样按歌名搜"
	case source.KindFolder:
		return "本机文件夹里的音乐，扫描子文件夹"
	case source.KindScript:
		return "本机的洛雪（LX）音源脚本（.js）。它列不出歌单：网易云放不了的那首歌，交给它按歌曲 ID 解析"
	}
	return ""
}

// sourcePage lists what the third-party sources hold, or says what to do
// about there being none.
func (a *app) sourcePage(c *ui.Context) {
	kept := a.sourceEntries()
	switch {
	case len(kept) == 0:
		a.sourceHint(c, "还没有添加第三方音源")
		return
	case !a.anySourceLists(kept) && len(a.sourceTracks) == 0:
		// Nothing is kept that lists songs of its own: the page is what
		// the sources are, which is still worth showing, rather than a
		// list of songs there are none of.
		a.sourceRows(c, kept)
		return
	}
	if a.sourceErr != "" {
		a.centered(c, a.sourceErr, func() { a.loadSource(true) })
		return
	}
	a.trackRows(c, a.sourceShown(), true, func(c *ui.Context) {
		a.sourceHeadingShown(c, kept)
	}, func(c *ui.Context) {
		a.sourceEndpoints(c, kept)
	})
}

// sourceEntries is the sources the settings keep, in the order they are
// kept: what the page names, whether or not it lists any song.
func (a *app) sourceEntries() []source.Entry {
	kept := make([]source.Entry, 0, len(a.settings.Sources))
	for _, e := range a.settings.Sources {
		if source.Label(e.Kind) != "" {
			kept = append(kept, e)
		}
	}
	return kept
}

// anySourceLists says whether any of the sources lists songs of its own:
// a script does not, and neither does an address that only searches.
func (a *app) anySourceLists(kept []source.Entry) bool {
	for _, e := range kept {
		if e.Lists() {
			return true
		}
	}
	return false
}

// sourceRows lists the sources themselves, under the songs they give: one
// row each, saying what it is and where it points, and what it answered
// when it was tested.
func (a *app) sourceRows(c *ui.Context, kept []source.Entry) {
	if len(kept) == 0 {
		return
	}
	p := a.pal
	a.section(c, "音源")
	ui.Column(c).Gap(0).Children(func() {
		for i := range kept {
			e := &kept[i]
			row := ui.Row(c).Key("source-row-"+e.ID).Padding(10, 4).Gap(12).
				AlignItems(ui.Center).MinHeight(44)
			row.Children(func() {
				ui.Box(c).Size(22, 22).Center().Children(func() {
					ui.Icon(c, sourceIcon(e.Kind)).Size(15, 15).TextColor(p.muted)
				})
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, e.Title()).FontSize(13.5).FontWeight(500).SingleLine()
					ui.Text(c, a.sourceRowNote(*e)).FontSize(12).TextColor(p.muted).SingleLine()
				})
				ui.Text(c, source.Label(e.Kind)).FontSize(11).FontWeight(600).
					TextColor(p.muted).Padding(1, 6).Radius(999).Border(1, p.hairline).SingleLine()
			})
			if row.Hovered() {
				row.Background(p.hover)
			}
			row.Radius(10)
		}
	})
}

// sourceRowNote is what a source's own row says: what testing it found, or
// why there is nothing to test yet.
func (a *app) sourceRowNote(e source.Entry) string {
	switch {
	case strings.TrimSpace(e.Value) == "":
		return "还没有填地址"
	case !e.Enabled:
		return "已关闭"
	}
	return a.sourceTestNote(e)
}

// sourceIcon is the mark of a kind of source, so that a row of them is
// read at a glance.
func sourceIcon(kind string) *ui.SVG {
	switch kind {
	case source.KindScript:
		return iconCode
	case source.KindFolder:
		return iconFolder
	}
	return iconRadio
}

// sourceHeadingShown names the page and holds its toolbar: the summary
// of what the sources list, the field to search the songs by, and the
// menu that sorts them.
func (a *app) sourceHeadingShown(c *ui.Context, kept []source.Entry) {
	a.sourceHeading(c)
	// The field is there whenever a source can be searched, even with
	// nothing listed yet: searching is what it is for.
	if len(a.sourceTracks) == 0 && !source.Searchable(a.settings.Sources) {
		return
	}
	ui.Box(c).Height(8)
	a.sourceToolbar(c)
	if strings.TrimSpace(a.sourceView.searched) != "" {
		a.sourceSearchNote(c)
	}
}

// sourceHeading names the page after the sources that fill it, with the
// buttons that list them again and that change where they are.
func (a *app) sourceHeading(c *ui.Context) {
	sub := sourceSummary(a.settings.Sources, len(a.sourceTracks), len(a.sourceFailed))
	p := a.pal
	ui.Row(c).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).Children(func() { a.heading(c, "第三方音源", sub) })
		ui.Row(c).Gap(4).Margin(0, 0, 24, 0).Children(func() {
			if iconButton(c.Key("refresh"), iconReload, 17, p.muted, "重新读取").Clicked() {
				a.loadSource(true)
			}
			// Every kind of source is added from here, where the songs it
			// gives are read: the button offers them all by name, so that
			// a folder of files on this machine is found without going
			// through the settings for it.
			iconButton(c.Key("add-source"), iconPlus, 17, p.muted, "添加音源").
				Menu(func(m *ui.Menu) {
					for _, k := range source.Kinds {
						// A folder of this machine's music has a page
						// of its own, beside this one: it is added
						// there, and listed there.
						if k.Kind == source.KindFolder {
							continue
						}
						kind := k.Kind
						if m.Item(sourceMenuLabel(k)).Chosen() {
							a.addSourceOfKind(kind)
						}
					}
				})
			if iconButton(c.Key("settings"), iconSettings, 17, p.muted, "设置").Clicked() {
				a.openSettings()
			}
		})
	})
}

// sourceSummary says what the page lists: how many sources it listed, how
// many tracks they hold, and how many could not be listed.
func sourceSummary(entries []source.Entry, tracks, failed int) string {
	on := 0
	for _, e := range entries {
		if e.On() {
			on++
		}
	}
	sub := fmt.Sprintf("%d 个音源 · %d 首", on, tracks)
	if failed > 0 {
		sub = fmt.Sprintf("%d 个音源 · %d 首 · %d 个读取失败", on, tracks, failed)
	}
	return sub
}

// sourceHint is what the page shows while there is nothing to list yet:
// what the sources are for, and the way to add one, since a listener who
// has none came here to add the first.
func (a *app) sourceHint(c *ui.Context, msg string) {
	top, bottom := a.insets(c)
	ui.Column(c).Grow(1).Padding(top, 0, bottom, 0).Center().Gap(14).Children(func() {
		ui.Text(c, "第三方音源").FontSize(22).FontWeight(700)
		ui.Text(c, msg).TextColor(a.pal.muted).FontSize(13)
		ui.Text(c, "网易云放不了的那几首歌，交给这里的音源去解析").TextColor(a.pal.faint).FontSize(12)
		ui.Row(c).Gap(8).Children(func() {
			ui.PrimaryButton(c.Key("add-source"), "添加音源").Menu(func(m *ui.Menu) {
				for _, k := range source.Kinds {
					// The music of this machine has its own page.
					if k.Kind == source.KindFolder {
						continue
					}
					kind := k.Kind
					if m.Item(sourceMenuLabel(k)).Chosen() {
						a.addSourceOfKind(kind)
					}
				}
			})
			if ui.Button(c.Key("open-settings"), "打开设置").Clicked() {
				a.openSettings()
			}
		})
	})
}

// chooseSourceFolder asks for the folder the source at i lists, and lists
// it once one is chosen.
func (a *app) chooseSourceFolder(i int) {
	a.pickSourcePath(i, true)
}

// chooseSourceFile asks for the audio source script at i: a Luoxue (LX)
// source is a file on this machine like any other, and is chosen the same
// way a folder is.
func (a *app) chooseSourceFile(i int) {
	a.pickSourcePath(i, false)
}

// pickSourcePath asks for where the source at i points — a folder of
// music, or a source script — and fills it in once one is chosen.
func (a *app) pickSourcePath(i int, folder bool) {
	win := a.settingsWin
	if i < 0 || i >= len(a.settings.Sources) {
		return
	}
	from := a.settings.Sources[i].Value
	title, label := "选择音乐文件夹", "选择"
	var filters []mygo.FileFilter
	if !folder {
		title, label = "选择音源文件", "导入"
		filters = []mygo.FileFilter{{Name: "音源脚本", Extensions: []string{"js", "mjs", "cjs"}}}
	}
	go func() {
		// The dialog blocks until it is dismissed: the app goes on drawing.
		picked, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       title,
			DefaultPath: from,
			ButtonLabel: label,
			Filters:     filters,
			Directory:   folder,
		})
		if err != nil || len(picked) == 0 {
			return
		}
		a.post(func() {
			if i >= len(a.settings.Sources) {
				return
			}
			a.settings.Sources[i].Value = picked[0]
			if !folder && strings.TrimSpace(a.settings.Sources[i].Name) == "" {
				// Named after the file it was read from, which is what
				// the listener will recognize it by.
				a.settings.Sources[i].Name = strings.TrimSuffix(filepath.Base(picked[0]), filepath.Ext(picked[0]))
			}
			a.changed()
		})
	}()
}

// newSourceID names a source the settings keep, one no other entry holds.
func newSourceID(entries []source.Entry) string {
	used := make(map[string]bool, len(entries))
	for _, e := range entries {
		used[e.ID] = true
	}
	for i := 1; ; i++ {
		id := fmt.Sprintf("s%d", i)
		if !used[id] {
			return id
		}
	}
}

// appendSource is the list of sources with one more, a source the
// listener fills in: a link, since a link is the shortest to type.
func appendSource(entries []source.Entry) []source.Entry {
	return append(entries, source.Entry{
		ID:      newSourceID(entries),
		Kind:    source.KindLink,
		Enabled: true,
	})
}

// dropSource is the list of sources without the one at i.
func dropSource(entries []source.Entry, i int) []source.Entry {
	if i < 0 || i >= len(entries) {
		return entries
	}
	return append(entries[:i:i], entries[i+1:]...)
}

// addSource adds a source to the settings.
func (a *app) addSource() {
	a.settings.Sources = appendSource(a.settings.Sources)
	a.changed()
}

// sourceMenuLabel is how a kind of source is offered on the page, named
// as the settings name it and marked so that the kinds are told apart at
// a glance.
func sourceMenuLabel(k source.Kind) string {
	switch k.Kind {
	case source.KindFolder:
		return "📁  本机文件夹"
	case source.KindAPI:
		return "🔗  HTTP 接口"
	case source.KindLink:
		return "🎵  链接（歌单 / 单曲）"
	case source.KindScript:
		return "📜  音源文件（洛雪 JS）"
	}
	return k.Label
}

// addSourceOfKind adds a source of the kind chosen on the page, where the
// songs it gives are read: a folder of files on this machine, or a script
// file, asks for where it is; an address is added empty, to be filled in
// where it is listed.
func (a *app) addSourceOfKind(kind string) {
	switch kind {
	case source.KindFolder:
		a.addSourceDir()
	case source.KindScript:
		a.addSourceScript()
	default:
		e := source.Entry{
			ID:      newSourceID(a.settings.Sources),
			Kind:    kind,
			Enabled: true,
		}
		a.settings.Sources = append(a.settings.Sources, e)
		a.changed()
	}
}

// addSourceDir asks for a folder of music on this machine and adds it as
// a source.
func (a *app) addSourceDir() {
	a.addSourceFromDialog(true, source.KindFolder)
}

// addSourceScript asks for a Luoxue (LX) source script on this machine and
// adds it as a source.
func (a *app) addSourceScript() {
	a.addSourceFromDialog(false, source.KindScript)
}

// addSourceFromDialog asks for where a source points — a folder of music,
// or a source script — and adds it, testing it at once so that the row it
// just added says whether what it names is a source at all.
func (a *app) addSourceFromDialog(folder bool, kind string) {
	win := a.settingsWin
	if win == nil {
		win = a.win
	}
	title, label := "选择音源文件", "导入"
	var filters []mygo.FileFilter
	if folder {
		title, label = "选择音乐文件夹", "选择"
	} else {
		filters = []mygo.FileFilter{{Name: "音源脚本", Extensions: []string{"js", "mjs", "cjs"}}}
	}
	go func() {
		picked, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       title,
			ButtonLabel: label,
			Filters:     filters,
			Directory:   folder,
		})
		if err != nil || len(picked) == 0 {
			return
		}
		path := picked[0]
		a.post(func() {
			e := source.Entry{
				ID:      newSourceID(a.settings.Sources),
				Kind:    kind,
				Value:   path,
				Enabled: true,
			}
			if !folder {
				// Named after the file it was read from, which is what
				// the listener will recognize it by.
				e.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			}
			a.settings.Sources = append(a.settings.Sources, e)
			a.changed()
			for i := range a.settings.Sources {
				if a.settings.Sources[i].ID == e.ID {
					a.testSource(i)
					break
				}
			}
		})
	}()
}

// removeSource takes the source at i out of the settings.
func (a *app) removeSource(i int) {
	a.settings.Sources = dropSource(a.settings.Sources, i)
	a.changed()
}

// sourceNote says what a source is: the kind it is, and where it points,
// or that it points nowhere yet.
func sourceNote(e source.Entry) string {
	label := source.Label(e.Kind)
	if label == "" {
		label = "未知的类型"
	}
	switch {
	case strings.TrimSpace(e.Value) == "":
		return label + " · 还没有填地址"
	case !e.Enabled:
		return label + " · 已关闭"
	}
	return label + " · " + shorten(e.Value, 48)
}

// sourcePlaceholder suggests what the address of a source looks like.
func sourcePlaceholder(kind string) string {
	switch kind {
	case source.KindAPI:
		return "https://…/search?keywords=" + source.SearchMark
	case source.KindLink:
		return "https://…/list.m3u"
	case source.KindFolder:
		return `D:\音乐`
	case source.KindScript:
		return `D:\音源\HYWmusic.js`
	}
	return ""
}

// sourceWhy is what the sources are for, said plainly: NetEase serves
// what it serves, and a third-party source is what plays the rest.
const sourceWhy = "网易云搜歌、看歌单、看评论都照旧；只有没版权、要会员、只有试听的那几首，" +
	"网易云不给音频流，这时候才轮到这里的音源：网易云知道是哪首歌（歌曲 ID），把它交给音源去解析。" +
	"想听全，加一个「音源文件（洛雪 JS）」，或者加一条带 " + SearchMarkWord + " 的搜索地址。"

// SearchMarkWord is where an address takes the words to search for, as
// the settings write it: the same mark source.SearchMark carries, said
// here so that the words beside a field cannot drift from what it reads.
const SearchMarkWord = source.SearchMark

// sourceAddressHelp is what the settings say about the addresses people
// paste: the shapes that work as they are, so that a source is filled in
// without reaching for a manual.
const sourceAddressHelp = "填浏览器里能打开、返回 JSON 的那个地址就行，前面不带 http:// 也认得，中文歌名不用手工转码。" +
	"地址里写 " + SearchMarkWord + " 就表示「按歌名搜」：播到网易云放不了的歌时，把歌名和歌手填进 " +
	SearchMarkWord + " 去问一次，拿它返回的第一首。" +
	"例如 …/search?keywords=" + SearchMarkWord + " 或 …/api.php?types=search&name=" + SearchMarkWord + "。" +
	"不写 " + SearchMarkWord + " 就是一张普通歌单，只列它自己的歌。" +
	"填好点「测试」，会立刻说读到几首"

// shorten cuts s down to n characters, and says it was cut.
func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
