//go:build linux

package proc

import "syscall"

// parentDeath makes the kernel kill the child when the thread that started it ends (mam crashing or being killed).
func parentDeath(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGKILL }
