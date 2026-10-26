//go:build !windows

package desktop

import "os/exec"

// detach does nothing off Windows: only the Windows desktop build installs updates, and this file
// exists so that the package builds and is tested on every platform.
func detach(*exec.Cmd) {}
