//go:build !windows

package ffmpeg

import "os/exec"

// hideWindow does nothing off Windows, where a child process opens no window.
func hideWindow(cmd *exec.Cmd) {}
