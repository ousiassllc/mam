//go:build windows

package facefusion

import (
	"os/exec"
	"syscall"
)

// createNoWindow is the CREATE_NO_WINDOW process creation flag (the syscall package has no name for it).
const createNoWindow = 0x08000000

// hideWindow keeps a child from opening a console window: the desktop build (-H windowsgui) has no console, so without this every
// run of Python would flash a new one (the same handling as internal/media).
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
