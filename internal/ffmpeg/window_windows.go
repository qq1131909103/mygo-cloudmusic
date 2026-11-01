//go:build windows

package ffmpeg

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps a console window from opening for the child process: an
// app with no console of its own would otherwise flash a black terminal for
// each ffmpeg it starts.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
