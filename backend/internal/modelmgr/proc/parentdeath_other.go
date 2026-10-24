//go:build !linux && !windows

package proc

import "syscall"

// parentDeath: only Linux has Pdeathsig; elsewhere the child ends with Stop (the bundled engine is a Windows feature).
func parentDeath(*syscall.SysProcAttr) {}
