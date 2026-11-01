package app

// Where the download button saves its songs: the Downloads folder, unless
// the listener picks another. It is one place for every song, rather than
// a question asked each time one is saved.

import (
	"strings"

	"github.com/egoist/mygo"
)

// downloadDirPath is where songs are saved, as the settings show it: the
// folder the listener picked, or the Downloads folder it is by default.
func (a *app) downloadDirPath() string {
	if dir := strings.TrimSpace(a.settings.DownloadDir); dir != "" {
		return dir
	}
	if d, err := mygo.App.Path(mygo.PathDownloads); err == nil {
		return d + "（默认）"
	}
	return "下载文件夹（默认）"
}

// chooseDownloadDir asks for the folder songs are saved in, and keeps it.
func (a *app) chooseDownloadDir() {
	win := a.settingsWin
	if win == nil {
		win = a.win
	}
	go func() {
		picked, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       "选择下载文件夹",
			ButtonLabel: "选择",
			Directory:   true,
			DefaultPath: a.settings.DownloadDir,
		})
		if err != nil || len(picked) == 0 {
			return
		}
		a.post(func() {
			a.settings.DownloadDir = picked[0]
			a.changed()
		})
	}()
}
