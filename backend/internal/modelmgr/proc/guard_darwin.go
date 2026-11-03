package proc

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// startGuard starts the guard for the child pid: mam's own binary again, in its own process group (so that a Ctrl-C in the
// terminal of mam does not end it before mam does), which kills the child's group when mam ends without running Close.
// macOS has no Pdeathsig.
func startGuard(childPID int) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, guardFlag, strconv.Itoa(os.Getpid()), strconv.Itoa(childPID)) // #nosec G204 -- mam's own binary, not a caller's command
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = []string{}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // the guard ends with the child; reaping it keeps no zombie
	return nil
}

// guardGrace is how long the guard lets the child end after SIGTERM before it kills the group.
const guardGrace = 5 * time.Second

func runGuard(parent, child int) error {
	return guard(parent, child, kqueueExit, signalLeftover, guardGrace)
}

// kqueueExit watches pid with kqueue (EVFILT_PROC, NOTE_EXIT): the kernel tells when the process ends, with no polling. A pid that
// does not exist (ESRCH) has ended already.
func kqueueExit(pid int) (<-chan struct{}, error) {
	done := make(chan struct{})
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	var ev unix.Kevent_t
	unix.SetKevent(&ev, pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
	ev.Fflags = unix.NOTE_EXIT
	if _, err := unix.Kevent(kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		_ = unix.Close(kq)
		if errors.Is(err, unix.ESRCH) {
			close(done)
			return done, nil
		}
		return nil, err
	}
	go func() {
		defer unix.Close(kq) //nolint:errcheck // the process ends soon after
		events := make([]unix.Kevent_t, 1)
		for {
			n, err := unix.Kevent(kq, nil, events, nil)
			switch {
			case errors.Is(err, unix.EINTR):
			case err != nil: // not an exit: closing done would make the guard kill a child that may be fine
				return
			case n > 0:
				close(done)
				return
			}
		}
	}()
	return done, nil
}
