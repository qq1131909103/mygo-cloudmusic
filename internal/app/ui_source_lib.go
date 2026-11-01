package app

// The third-party sources' page, as a library of its own: the songs the
// sources list, and the ones they can be asked for by name. A listener
// who keeps sources has a library beside NetEase's, and looks through it
// the way they look through NetEase's.

import (
	"net/url"
	"sort"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// sourceOrders are the ways the page's songs may be sorted, as the menu
// names them.
var sourceOrders = []struct {
	kind  string
	label string
}{
	{"default", "默认顺序"},
	{"name", "按歌名"},
	{"artist", "按歌手"},
	{"source", "按音源"},
}

// sourceView is what the page is showing: the words searched for in the
// sources' interfaces, what that found, and how the songs are sorted.
type sourceView struct {
	// query is what is typed in the field.
	query string
	// asked is the source searched, by its id, or "" for every one that
	// can be: several interfaces may be kept, and a listener may want one.
	asked string
	// searched is the words last asked for, which is what the results are
	// of: typing does not search, Enter does.
	searched string
	// found is what the search answered with, from the interfaces.
	found []netease.Track
	// failed are the sources that could not answer, and why.
	failed []string
	// busy is a search on its way.
	busy bool
	// order is how the songs are sorted.
	order string
}

// sourceShown is the songs the page shows: what the interfaces answered
// with when there has been a search, and the songs the sources list when
// there has not, in the order chosen.
func (a *app) sourceShown() []netease.Track {
	v := &a.sourceView
	tracks := a.sourceTracks
	if strings.TrimSpace(v.searched) != "" {
		tracks = v.found
	}
	out := append([]netease.Track(nil), tracks...)
	// Sorted as a reader of the language sorts: comparing the bytes would
	// put every Chinese title after every Latin one.
	less := collate.New(language.SimplifiedChinese).CompareString
	switch v.order {
	case "name":
		sort.SliceStable(out, func(i, j int) bool { return less(out[i].Name, out[j].Name) < 0 })
	case "artist":
		sort.SliceStable(out, func(i, j int) bool {
			return less(out[i].ArtistNames(), out[j].ArtistNames()) < 0
		})
	case "source":
		sort.SliceStable(out, func(i, j int) bool { return less(out[i].Source, out[j].Source) < 0 })
	}
	return out
}

// filterTracks keeps the songs the words name: their title, who sings
// them, their album, or the source they came from. All the words must be
// there, so that a longer query narrows rather than widens.
func filterTracks(tracks []netease.Track, q string) []netease.Track {
	words := strings.Fields(strings.ToLower(q))
	if len(words) == 0 {
		return tracks
	}
	var out []netease.Track
	for _, t := range tracks {
		hay := strings.ToLower(t.Name + " " + t.ArtistNames() + " " + t.Album.Name + " " + t.Source)
		all := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				all = false
				break
			}
		}
		if all {
			out = append(out, t)
		}
	}
	return out
}

// sourceSearch asks the sources' interfaces for the words: the songs come
// back from wherever those keep them, which is what searching the sources
// means. A folder or a script cannot be searched, and neither is asked.
func (a *app) sourceSearch(words string) {
	words = strings.TrimSpace(words)
	v := &a.sourceView
	if words == "" {
		// Cleared: the page goes back to what the sources list.
		v.searched, v.found, v.failed, v.busy = "", nil, nil, false
		return
	}
	searchable := a.searchableEntries()
	if len(searchable) == 0 {
		v.searched, v.found, v.busy = words, nil, false
		v.failed = []string{"没有能搜索的音源：只有带 {keyword} 的接口能搜，洛雪脚本和本机文件夹不能"}
		return
	}
	v.searched, v.busy, v.failed = words, true, nil
	gen := a.sourceGen
	go func() {
		found, failed := source.Search(searchable, words)
		a.post(func() {
			if gen != a.sourceGen {
				return
			}
			v := &a.sourceView
			if v.searched != words {
				return // another search took over
			}
			v.found, v.failed, v.busy = found, failed, false
			a.redraw()
		})
	}()
}

// searchableEntries are the sources that can be searched: the addresses
// carrying {keyword}. They are what a search asks.
func (a *app) searchableEntries() []source.Entry {
	var out []source.Entry
	for _, e := range a.settings.Sources {
		if e.Searchable() && (a.sourceView.asked == "" || e.ID == a.sourceView.asked) {
			out = append(out, e)
		}
	}
	return out
}

// askedSource names the source the field searches, or "" when it searches
// every one that can be searched.
func (a *app) askedSource() string {
	for _, e := range a.settings.Sources {
		if e.ID == a.sourceView.asked && e.Searchable() {
			return e.Title()
		}
	}
	return ""
}

// sourceOrderLabel is what the sort in force is called.
func (a *app) sourceOrderLabel() string {
	for _, o := range sourceOrders {
		if o.kind == a.sourceView.order {
			return o.label
		}
	}
	return sourceOrders[0].label
}

// sourceToolbar is the row above the songs: the field that searches the
// sources' interfaces, and the menu that sorts what is shown.
func (a *app) sourceToolbar(c *ui.Context) {
	p := a.pal
	v := &a.sourceView
	ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 0, 10, 0).Children(func() {
		field := ui.Row(c).Height(32).PaddingX(10).Gap(7).Radius(9).AlignItems(ui.Center).Grow(1).Basis(0).MinWidth(0)
		field.Background(p.hover).Border(1, p.hairline)
		field.Children(func() {
			ui.Icon(c, iconSearch).Size(14, 14).TextColor(p.muted)
			in := ui.TextInput(c.Key("source-search"), &v.query).
				Placeholder("搜索音源接口里的歌，回车").
				FontSize(13).Label("搜索音源").Grow(1).MinWidth(0)
			// Enter searches; the field is not a filter over the list,
			// which is what it used to be, and what looked like it was
			// searching the music on this machine.
			if in.Submitted() {
				a.sourceSearch(v.query)
			}
		})
		if strings.TrimSpace(v.searched) != "" {
			// Cleared: the page goes back to what the sources list.
			if iconButton(c.Key("source-search-clear"), iconX, 14, p.muted, "清除搜索").Clicked() {
				v.query, v.searched, v.found, v.failed, v.busy = "", "", nil, nil, false
			}
		}
		// Which source is searched: several interfaces may be kept, and a
		// script cannot be searched at all, which the menu says rather than
		// leaving it out as if it were no source.
		name := a.askedSource()
		if name == "" {
			name = "全部音源"
		}
		iconButton(c.Key("source-pick"), iconFilter, 15, p.muted, "搜索音源："+name).
			Menu(func(m *ui.Menu) {
				if m.Item("全部音源").Checked(v.asked == "").Chosen() {
					v.asked = ""
					a.retrySearch()
				}
				for _, e := range a.settings.Sources {
					if !e.On() || e.Kind == source.KindScript || e.Kind == source.KindFolder {
						continue
					}
					ok := e.Searchable()
					it := m.Item(e.Title())
					if ok {
						it = it.Checked(v.asked == e.ID)
					} else {
						it = it.Disabled(true)
					}
					if ok && it.Chosen() {
						v.asked = e.ID
						a.retrySearch()
					}
				}
				// The scripts are named too, saying why they cannot be
				// asked: their protocol answers for one song at a time, by
				// the id NetEase gave it, and has no search in it.
				for _, e := range a.settings.Sources {
					if e.On() && e.Kind == source.KindScript {
						m.Separator()
						m.Item(e.Title() + "（脚本不能搜索）").Disabled(true)
					}
				}
			})
		iconButton(c.Key("source-order"), iconUpDown, 15, p.muted, "排序："+a.sourceOrderLabel()).
			Menu(func(m *ui.Menu) {
				for _, o := range sourceOrders {
					if m.Item(o.label).Checked(o.kind == v.order).Chosen() {
						v.order = o.kind
					}
				}
			})
	})
}

// retrySearch searches again for the words last asked for, after the
// source asked changed.
func (a *app) retrySearch() {
	v := &a.sourceView
	if strings.TrimSpace(v.searched) == "" {
		return
	}
	v.searched = ""
	a.sourceSearch(v.query)
}

// sourceSearchNote says what a search found, or why it found nothing.
func (a *app) sourceSearchNote(c *ui.Context) {
	v := &a.sourceView
	p := a.pal
	switch {
	case v.busy:
		ui.Text(c, "正在搜索…").FontSize(12).TextColor(p.muted).Padding(0, 0, 10, 0)
	case len(v.failed) > 0 && len(v.found) == 0:
		ui.Text(c, "搜索没有结果："+strings.Join(v.failed, "；")).FontSize(12).TextColor(p.accent).
			Padding(0, 0, 10, 0)
	case len(v.failed) > 0:
		ui.Textf(c, "找到 %d 首（%s 没能搜）", len(v.found), strings.Join(v.failed, "；")).
			FontSize(12).TextColor(p.muted).Padding(0, 0, 10, 0)
	default:
		ui.Textf(c, "从音源接口搜到 %d 首", len(v.found)).FontSize(12).TextColor(p.muted).Padding(0, 0, 10, 0)
	}
}

// sourceEndpoints lists the sources' interfaces: where each is asked, so
// that a listener can see what their music is coming from, and read the
// address itself. The ones that can be searched are named as such, since
// they are what the field above searches.
func (a *app) sourceEndpoints(c *ui.Context, kept []source.Entry) {
	p := a.pal
	a.section(c, "音源接口")
	ui.Column(c).Gap(0).Children(func() {
		for i := range kept {
			e := &kept[i]
			addr := strings.TrimSpace(e.Value)
			shown := ""
			switch e.Kind {
			case source.KindScript:
				shown = "脚本：" + shorten(addr, 72)
			case source.KindFolder:
				shown = "文件夹：" + shorten(addr, 72)
			default:
				shown = shorten(addr, 88)
			}
			if u, err := url.Parse(addr); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
				shown = u.Host + shorten(u.RequestURI(), 60)
			}
			row := ui.Row(c).Key("endpoint-"+e.ID).Padding(8, 4).Gap(10).AlignItems(ui.Center).MinHeight(38)
			row.Children(func() {
				ui.Box(c).Size(16, 16).Center().Children(func() {
					ui.Icon(c, sourceIcon(e.Kind)).Size(13, 13).TextColor(p.muted)
				})
				ui.Text(c, e.Title()).FontSize(12.5).FontWeight(500).Shrink(0).SingleLine()
				// What it is asked for: a search by name, or a song by id.
				switch {
				case e.Searchable():
					ui.Text(c, "可搜索").FontSize(10.5).FontWeight(600).TextColor(p.accent).
						Padding(1, 5).Radius(4).Border(1, p.accent.Alpha(0.5)).Shrink(0)
				case e.Kind == source.KindScript:
					ui.Text(c, "按歌曲 ID 解析").FontSize(10.5).FontWeight(600).TextColor(p.muted).
						Padding(1, 5).Radius(4).Border(1, p.hairline).Shrink(0)
				case e.Kind == source.KindFolder:
					ui.Text(c, "本机文件").FontSize(10.5).FontWeight(600).TextColor(p.muted).
						Padding(1, 5).Radius(4).Border(1, p.hairline).Shrink(0)
				}
				ui.Text(c, shown).FontSize(11.5).TextColor(p.faint).SingleLine().Grow(1).Basis(0).MinWidth(0).
					Tooltip(addr)
			})
			if row.Hovered() {
				row.Background(p.hover)
			}
			row.Radius(8)
		}
	})
}
