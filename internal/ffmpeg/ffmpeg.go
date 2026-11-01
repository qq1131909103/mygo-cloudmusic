// Package ffmpeg finds an ffmpeg on this machine, and reports the formats
// it decodes: the ones the app has no decoder of its own for — a WMA in
// an ASF container, an M4A of AAC in an MP4 one. Nothing of ffmpeg is
// taken for granted: an app without one plays the formats it decodes
// itself, and says of the others that they need one.
package ffmpeg

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// exts are the extensions this asks ffmpeg for: the formats a listener's
// own library is in which the app does not decode itself.
var exts = []string{".wma", ".m4a", ".aac", ".opus", ".ape", ".aiff", ".aif", ".alac", ".wv"}

// Handled says whether a file of the format at path is one ffmpeg is
// asked for, rather than one the app decodes itself.
func Handled(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range exts {
		if e == ext {
			return true
		}
	}
	return false
}

var (
	once   sync.Once
	found  string
	looked bool
)

// Path returns where ffmpeg is, or "" when there is none: the executable
// beside the app first, so that a copy shipped with it is what is used,
// and then the one on the PATH.
func Path() string {
	once.Do(func() {
		looked = true
		found = look()
	})
	return found
}

// SetPath names the executable to use, as the app finds it before the app
// is shown: a build that ships one beside itself names it here.
func SetPath(p string) {
	once.Do(func() {})
	looked, found = true, p
}

// Available reports whether there is an ffmpeg to ask.
func Available() bool { return Path() != "" }

func look() string {
	// Beside the app: where a bundled copy is, and where a listener who
	// put one there means it to be used.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, name := range []string{"ffmpeg", "ffmpeg.exe"} {
			for _, sub := range []string{"", "tools", "bin"} {
				p := filepath.Join(dir, sub, name)
				if isFile(p) {
					return p
				}
			}
		}
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	if runtime.GOOS == "windows" {
		for _, p := range windowsPaths() {
			if isFile(p) {
				return p
			}
		}
	}
	return ""
}

// windowsPaths are where a copy is usually installed, by winget or by
// hand, when the PATH the app was started with does not carry it: a
// window opened from the desktop inherits no shell's PATH.
func windowsPaths() []string {
	var out []string
	local := os.Getenv("LOCALAPPDATA")
	if local != "" {
		links := filepath.Join(local, "Microsoft", "WinGet", "Links", "ffmpeg.exe")
		out = append(out, links)
		pkgs := filepath.Join(local, "Microsoft", "WinGet", "Packages")
		if entries, err := os.ReadDir(pkgs); err == nil {
			for _, e := range entries {
				if !strings.HasPrefix(strings.ToLower(e.Name()), "gyan.ffmpeg") {
					continue
				}
				// The archive unpacks into a versioned folder holding bin/.
				root := filepath.Join(pkgs, e.Name())
				if subs, err := os.ReadDir(root); err == nil {
					for _, sub := range subs {
						p := filepath.Join(root, sub.Name(), "bin", "ffmpeg.exe")
						out = append(out, p)
					}
				}
				out = append(out, filepath.Join(root, "bin", "ffmpeg.exe"))
			}
		}
	}
	for _, dir := range []string{`C:\ffmpeg\bin`, `C:\Program Files\ffmpeg\bin`} {
		out = append(out, filepath.Join(dir, "ffmpeg.exe"))
	}
	return out
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// HideWindow keeps the console window of cmd, an ffmpeg this app starts, from
// showing on Windows.
func HideWindow(cmd *exec.Cmd) { hideWindow(cmd) }
