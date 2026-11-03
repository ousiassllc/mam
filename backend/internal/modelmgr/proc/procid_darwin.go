package proc

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"
)

const szomb = 5 // p_stat of a process that has ended and is not reaped yet

// probeProcess reads the start time (sysctl kern.proc.pid) and the command line (sysctl kern.procargs2) of pid.
func probeProcess(pid int) (processID, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(err, unix.ESRCH) || (err == nil && kp.Proc.P_stat == szomb) {
		return processID{}, errNoProcess
	}
	if err != nil {
		return processID{}, err
	}
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if errors.Is(err, unix.ESRCH) || errors.Is(err, unix.EINVAL) { // gone since the first call
		return processID{}, errNoProcess
	}
	if err != nil {
		return processID{}, err
	}
	argv, err := parseProcArgs2(raw)
	if err != nil {
		return processID{}, err
	}
	t := kp.Proc.P_starttime
	return processID{PID: pid, Start: fmt.Sprintf("%d.%06d", t.Sec, t.Usec), Cmdline: strings.Join(argv, " ")}, nil
}
