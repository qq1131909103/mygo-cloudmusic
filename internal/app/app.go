package app

import (
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/source"
)

type pageKind int

// fallbackSet is the sources the player falls back on, kept where both
// the settings, which change them, and the player, which asks from its
// own thread, reach them safely. Held by pointer: the app is copied in
// tests, and a lock inside it may not be.
type fallbackSet struct {
	mu      sync.Mutex
	entries []source.Entry
}

// list is the sources as they are now.
func (f *fallbackSet) list() []source.Entry {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.entries
}

// keep is the sources as they are now.
func (f *fallbackSet) keep(entries []source.Entry) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append([]source.Entry(nil), entries...)
}

// hasAnswers says whether the listener keeps anything to fall back on:
// without it a song NetEase will not serve is a song there is no point
// offering, and with it every song they can name plays.
func (f *fallbackSet) hasAnswers() bool {
	return source.AnswersSongs(f.list())
}

// canPlay says whether the track at tr can be played: NetEase plays it,
// or a source the listener keeps can find where it plays instead. Without
// such a source this is what it always was, so that a listener who keeps
// none sees the songs NetEase will not serve the way they always did.
func (a *app) canPlay(tr *netease.Track) bool {
	if path, local := source.LocalPath(tr.PlayURL); local {
		// A file of this machine plays if the app decodes its format: the
		// ones it does not — the rest of a library, which a folder lists
		// all the same — are seen and left alone.
		return source.Decodable(path)
	}
	if tr.Playable() {
		return true
	}
	// What is left is a song NetEase withholds — one that asks for a
	// membership, or a preview. Those are the ones a source answers for.
	return a.fallbackSources.hasAnswers()
}

const (
	pageHome pageKind = iota
	pageDaily
	pageCharts
	pagePlaylist
	pageSearch
	pageFM
	pageArtist
	pageAlbum
	pageUser
	pagePodcast
	pagePodcasts
	pageSource
	// pageLocal is the music on this machine, which is a page of its own
	// rather than a source among the others: it is where a listener goes
	// to add a folder of their own music, and to hear it.
	pageLocal
)

type route struct {
	page pageKind
	// id is the playlist, artist, album, user or podcast the page shows.
	id int64
	// query and kind are what a search page searched for.
	query string
	kind  netease.SearchType
}

// app is the state both windows show. Everything in it is read and changed
// on the main thread: by the views, and by goroutines through post.
type app struct {
	win   *mygo.Window
	notch *notch
	tray  *mygo.Tray // the icon in the menu bar or notification area, while it shows
	art   *artCache
	pl    *player.Player
	pal   palette

	route   route
	history []route
	forward []route // the pages gone back from, the last first to go to

	profile      *netease.Profile
	mine, saved  []netease.Playlist // the listener's playlists: made, and saved
	savingPl     map[int64]bool     // the playlists whose saving or its end is on its way
	recPlaylists []netease.Playlist // the day's playlists picked for the listener
	login        loginState
	accountOpen  bool           // the dropdown of the account row shows
	naming       nameDialog     // the name of a playlist asked for
	deleting     deleteDialog   // whether to delete a playlist, asked
	liked        map[int64]bool // the songs the listener likes
	toast        string         // a message to show once
	picked       pickedUp       // the song a login picked up, waiting to play

	// The episodes the listener likes, and doesn't, as far as NetEase has
	// answered, and those asked about: see ui_like.go.
	likedPrograms, askedPrograms map[int64]bool

	daily       []netease.Track
	dailySpin   spinState // the record on the daily card
	dailyBars   barState  // the bars on its play button
	featured    []netease.Playlist
	charts      []netease.Playlist
	homeErr     string
	homeLoading bool
	homeAt      time.Time // when what the home page shows was asked for

	// What the third-party sources hold, the ones the settings keep: see
	// ui_source.go.
	sourceTracks  []netease.Track
	sourceFailed  []string // the sources that could not be listed, and why
	sourceLoading bool
	sourceErr     string
	sourceFor     string // the settings the tracks were listed for
	sourceGen     int    // counts lists, to drop what a stale one gives
	// What the folders of this machine hold, listed apart from the other
	// sources: a folder is read in milliseconds, while an endpoint may take
	// seconds, and the page of this machine's music should not wait for it.
	localSongs   []netease.Track
	localFailed  []string
	localFor     string
	localGen     int
	localLoading bool
	// What the sources" page is showing of their songs: the words searched
	// for, and how they are sorted: see ui_source_lib.go.
	sourceView sourceView
	// localSearch is what the page of this machine`s music filters by: see
	// ui_local.go.
	localSearch string
	// sidecarBusy says a fetch of the covers and lyrics of this machine`s
	// music is running: see ui_local.go.
	sidecarBusy bool
	// What the settings' own test of a source found, by the entry it
	// tested, and the ones a test is running for: see ui_source.go.
	sourceTests map[string]string
	sourceBusy  map[string]bool
	// What the player is given to fall back on when NetEase will not
	// serve a song: the sources the settings keep, copied here because
	// the player asks from its own thread while the settings are edited
	// on this one. Held by pointer: the app is copied in tests, and a
	// lock inside it may not be.
	fallbackSources *fallbackSet

	// What is being saved to the Downloads folder, and what the last one
	// did: see ui_download.go. Held by pointer, as the fallback sources
	// are: the app is copied in tests, and a lock inside it may not be.
	downloads *downloadState

	playlists  map[int64]*netease.Playlist
	loadingPl  map[int64]bool
	plErr      map[int64]string
	artists    map[int64]*artistData
	artistTab  int // what an artist's page lists: artistHot, artistLatest, artistAlbums or artistEvents
	albums     map[int64]*albumData
	users      map[int64]*userData
	podcasts   map[int64]*podcastData
	podcastTab *podcastTab // the podcasts tab's lists
	trackList  ui.ListState
	// Where the page and the sidebar are scrolled, kept while Now Playing
	// covers them, unbuilt.
	pageScroll ui.ScrollState
	navScroll  ui.ScrollState
	scrolled   route // the page trackList and pageScroll are of

	mineCollapsed, savedCollapsed bool

	query       string
	searchBox   ui.Handle
	searches    map[route]*searchData // the results of each search, by its page
	searched    route                 // the search last shown, which the sidebar goes back to
	suggestions suggestState          // what the search field's query may be after

	nowPlaying  bool
	npOpen      bool      // Now Playing as it last showed: open or not
	npChangedAt time.Time // when it last opened or closed
	npPane      int       // paneLyrics, paneQueue, paneStage, paneComments, paneProgram or paneCaptions
	npPaneTop   float32   // how far the cover beside the pane starts below its top
	lyricScroll ui.ScrollState
	lyricGlide  lyricGlide
	lyricAt     int
	lyricTrack  int64
	queueList   ui.ListState
	queueAt     int // the row of the queue pane last scrolled to, -1 while it is hidden
	comments    commentState
	tabCount    rollState // the count on the comments' tab, as it rolls

	fmMode        int    // the FM mode chosen, in fmModes
	radioStarting string // the radio asked for its first songs, or ""

	scrub      float64
	scrubbing  bool
	volume     float64
	volumeOpen bool // the volume popover of the player bar shows
	mutedFrom  float64

	rowBars barState
	// underTop and underBottom say whether the page has content under its
	// bars, which show their scroll edges then.
	underTop, underBottom bool
	barScrub              scrubState
	npScrub               scrubState
	backdropCache         backdropCache // the backdrop of Now Playing, as last painted
	npArt                 *artwork      // the cover whose colors Now Playing shows
	npCoverState          coverState    // the cover of Now Playing, as it is tilted and flicked
	wallState             wallState     // the wall of the queue's covers, as it is dragged
	quiet                 quietState    // the quiet parts of the song playing, for its hot comments
	captions              captionState  // the episode playing's captions, and the model that writes them
	sig                   playerSig
	quitting              bool

	settings    settings
	settingsWin *mygo.Window
	cacheUsed   int64 // the bytes the song cache takes, -1 until measured
	updates     bool  // the update plugin is used (useUpdater)

	restored     bool   // playback picked up where it was left
	savedSession string // the session as last kept
	sessionDue   bool   // a save of the session is due
}

// post runs fn on the main thread and redraws both windows. Without a
// window, as in tests, it drops fn.
func (a *app) post(fn func()) {
	if a.win == nil {
		return
	}
	a.win.Update(func() {
		fn()
		a.redraw()
	})
}

func (a *app) redraw() {
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.notch != nil && a.notch.win != nil {
		a.notch.win.Invalidate()
	}
}

// shotHook, when set, drives the app to check its design.
var shotHook func(a *app)

func (a *app) showMain() {
	if a.win != nil {
		if a.win.IsMinimized() {
			a.win.Restore()
		}
		a.win.Show()
		a.win.Focus()
		// From the menu bar alone the app may be inactive.
		mygo.App.Focus()
	}
}

// Run runs the app until it quits.
func Run() error {
	a := &app{
		playlists:  map[int64]*netease.Playlist{},
		loadingPl:  map[int64]bool{},
		plErr:      map[int64]string{},
		artists:    map[int64]*artistData{},
		albums:     map[int64]*albumData{},
		users:      map[int64]*userData{},
		podcasts:   map[int64]*podcastData{},
		podcastTab: &podcastTab{},
		searches:   map[route]*searchData{},
		lyricAt:    -1,
		queueAt:    -1,
		cacheUsed:  -1,
	}
	moveOldData()
	a.useUpdater()
	mygo.App.WhenReady(func() {
		a.settings = loadSettings()
		a.settings.applyAppearance()
		a.npPane = a.lyricsPaneChosen()
		// Before the window: on Windows it lays out its content as it is
		// made, and the view asks the player what plays.
		a.art = newArtCache(a.post)
		a.pl = player.New(a.post)
		a.pl.Quality = func() string { return a.settings.Quality }
		a.pl.Normalize = func() bool { return a.settings.Normalize }
		// A song NetEase will not serve is a song the listener's own
		// sources are asked for: a Luoxue script kept on this machine,
		// or an address that takes the words to search for. What the
		// settings keep is copied, as the player asks from its own
		// thread while the settings are edited on this one.
		// What the download button saves, and where: the one list beside the
		// player's, since both run on this thread. See ui_download.go.
		a.downloads = &downloadState{}

		a.fallbackSources = &fallbackSet{}
		a.fallbackSources.keep(a.settings.Sources)
		a.pl.Fallback = func(t netease.Track, level string) (source.Answer, error) {
			return source.Fallback(a.fallbackSources.list(), t, level)
		}
		a.pl.Cache = newSongCache(a.settings.CacheMB)
		a.pl.Changed = func() {
			a.keepSession()
			a.syncCaptions()
		}
		// A file of this machine that came with no cover and no lyrics has
		// the song it is looked up for them: the app is what asks NetEase,
		// and the player is what finds the file playing without them.
		a.pl.LocalMeta = a.localMeta
		// How far the lyrics are moved later, as the listener set it,
		// asked again whenever the buttons beside them are pressed.
		a.wireShift()
		// Covers fetched for files of this machine are kept where a scan
		// reads them back, so that a restart shows them again.
		source.SetCoverCache(coverDirName())
		a.volume = a.settings.Volume
		a.pl.SetVolume(a.volume)
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         mygo.App.Name(),
			Width:         1180,
			Height:        780,
			MinWidth:      880,
			MinHeight:     580,
			StateKey:      "main",
			TitleBarStyle: mygo.TitleBarHidden,
			// As tall as the edge over a page (Linux, Windows).
			TitleBarHeight: buttonsHeight,
			// Inset from the corner, within the sidebar, as macOS 26's
			// apps with a sidebar place them.
			TrafficLightPosition: &mygo.Point{X: 20, Y: 20},
			Vibrancy:             mygo.VibrancySidebar,
			BackgroundColor:      "light-dark(#ffffff, #141416)",
			Content:              ui.View(a.view),
		})
		a.setMenu()
		a.boot()
		// The notch player is macOS's, over its menu bar: elsewhere it would
		// hang at the top of the screen as a black box.
		if runtime.GOOS == "darwin" {
			a.notch = newNotch(a)
		}
		// From the menu bar alone, the Dock icon shows while the window
		// does.
		a.syncDock()
		a.win.OnShow(a.syncDock)
		a.win.OnHide(a.syncDock)
		// Shown or switched to the morning after, it shows the new day's.
		a.win.OnFocus(a.freshenHome)
		a.applyTray()
		if shotHook != nil {
			shotHook(a)
		}
		// Closing the window hides it: music plays on, and the Dock icon
		// or the tray's brings it back. A Linux desktop without a tray
		// has neither, so there closing it quits.
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if a.quitting {
				return
			}
			e.PreventDefault()
			if runtime.GOOS == "darwin" || a.tray != nil {
				a.win.Hide()
			} else {
				mygo.App.Quit()
			}
		})
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.quitting = true
		a.settings.Volume = a.volume
		a.settings.save()
		a.saveSession()
		if write := a.transcriptWrite(); write != nil {
			write()
		}
	})
	// Windows leaves an icon in the notification area after its app until
	// the pointer passes over it.
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(bool) { a.showMain() })
	return mygo.App.Run()
}
