//go:build windows

package desktop

import (
	"os/exec"
	"syscall"
)

// Process creation flags (the syscall package names neither).
const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// detach gives the installer no console and a process group of its own, so that the end of this
// process (and the console control events it may get on the way) does not reach it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
