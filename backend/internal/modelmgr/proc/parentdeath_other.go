//go:build !linux && !windows

package proc

import "syscall"

// parentDeath: only Linux has Pdeathsig; elsewhere (macOS) the child ends with Stop or Close, which signal its process group. A mam that is
// killed without running Close leaves the engine running on macOS (spec/architecture/model-manager/model-manager.md §3.7).
func parentDeath(*syscall.SysProcAttr) {}
