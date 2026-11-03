//go:build !windows

package proc

import (
	"fmt"
	"time"
)

// guardFlag is the first argument of the guard process: `<mam binary> --mam-proc-guard <mam pid> <child pid>`. The guard is
// mam's own binary started again (never a program the caller names): it starts nothing, it only waits for mam to end and
// then signals the process group of the child it was told about (spec/architecture/model-manager/model-manager.md §3.4).
const guardFlag = "--mam-proc-guard"

// exitWatch returns a channel that is closed when the process pid has ended (at once for a pid that does not exist).
type exitWatch func(pid int) (<-chan struct{}, error)

// guard waits until parent has ended and then ends the group of child: interrupt first, then (after grace) kill. It returns
// without touching the group when child ends first (a stopped child: its pid, and so its group id, may be somebody else's by now).
// The OS-specific parts are watch (kqueue on macOS) and signal (a signal to the group led by the pid).
func guard(parent, child int, watch exitWatch, signal func(pid int, force bool), grace time.Duration) error {
	parentGone, err := watch(parent)
	if err != nil {
		return fmt.Errorf("watch parent %d: %w", parent, err)
	}
	childGone, err := watch(child)
	if err != nil {
		return fmt.Errorf("watch child %d: %w", child, err)
	}
	select {
	case <-childGone:
		return nil
	case <-parentGone:
	}
	select { // both may be ready: the child that is gone is not signalled
	case <-childGone:
		return nil
	default:
	}
	signal(child, false)
	select {
	case <-childGone:
	case <-time.After(grace):
		signal(child, true)
	}
	return nil
}
