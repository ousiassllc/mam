//go:build !windows

package desktop

import (
	"os/exec"
	"syscall"
)

// detach gives the helper a session of its own off Windows (the macOS update helper), so that the end of this
// process, or of the terminal it was started from (SIGHUP), does not reach it. On Windows only the installer is
// detached (update_windows.go).
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
