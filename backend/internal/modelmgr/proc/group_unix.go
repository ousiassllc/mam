//go:build !windows

package proc

import (
	"os"
	"os/exec"
	"syscall"
)

// group is the process group of the child: everything it starts is in it and can be signalled together.
type group struct{ pgid int }

func newGroup() (*group, error) { return &group{}, nil }

// prepare puts the child in its own process group (and, on Linux, asks the kernel to kill it when mam dies).
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	parentDeath(cmd.SysProcAttr)
}

func (g *group) add(p *os.Process) error { g.pgid = p.Pid; return nil }

// interrupt asks the group to end (SIGTERM).
func (g *group) interrupt() { g.signal(syscall.SIGTERM) }

// kill ends the group at once.
func (g *group) kill() { g.signal(syscall.SIGKILL) }

func (g *group) signal(sig syscall.Signal) {
	if g.pgid > 0 {
		_ = syscall.Kill(-g.pgid, sig)
	}
}

func (g *group) close() {}

// signalLeftover signals the process group led by pid (a child that was started with Setpgid): SIGTERM, or SIGKILL when force. A pid that
// leads no group of its own is left alone, so that a pid that is somebody else's by now never reaches another group (mam's, the shell's).
func signalLeftover(pid int, force bool) {
	if pgid, err := syscall.Getpgid(pid); err != nil || pgid != pid || pid <= 1 {
		return
	}
	if force {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}
