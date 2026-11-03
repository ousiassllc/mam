//go:build !linux && !windows

package proc

import "syscall"

// parentDeath: only Linux has Pdeathsig. On macOS the child ends with Stop or Close (they signal its process group), and when mam is killed
// without Close the guard process (guard.go) ends it; the pid file (pidfile.go) is the second net.
func parentDeath(*syscall.SysProcAttr) {}
