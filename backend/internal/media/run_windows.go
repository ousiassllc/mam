//go:build windows

package media

import (
	"os/exec"
	"syscall"
)

// createNoWindow is the CREATE_NO_WINDOW process creation flag (the syscall package has no name for it).
const createNoWindow = 0x08000000

// hideWindow keeps a child from opening a console window. The desktop build (-H windowsgui) has no
// console, so without this every ffmpeg / ffprobe run would flash a new one.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
