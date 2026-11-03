package proc

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// probeProcess reads the start time (field 22 of /proc/<pid>/stat, in clock ticks since boot) and the command line of pid.
func probeProcess(pid int) (processID, error) {
	dir := "/proc/" + strconv.Itoa(pid)
	stat, err := os.ReadFile(dir + "/stat") // #nosec G304 -- /proc
	if errors.Is(err, os.ErrNotExist) {
		return processID{}, errNoProcess
	}
	if err != nil {
		return processID{}, err
	}
	// "<pid> (<comm>) <state> ...": comm may hold spaces and parentheses, so the fields start after the last ") ".
	i := strings.LastIndex(string(stat), ") ")
	if i < 0 {
		return processID{}, errors.New("unexpected /proc stat")
	}
	fields := strings.Fields(string(stat)[i+2:])
	if len(fields) < 20 {
		return processID{}, errors.New("unexpected /proc stat")
	}
	if fields[0] == "Z" && !hasOtherThreads(dir, pid) {
		return processID{}, errNoProcess
	}
	cmdline, err := os.ReadFile(dir + "/cmdline") // #nosec G304 -- /proc
	if errors.Is(err, os.ErrNotExist) {
		return processID{}, errNoProcess
	}
	if err != nil {
		return processID{}, err
	}
	return processID{PID: pid, Start: fields[19], Cmdline: strings.TrimSpace(strings.ReplaceAll(string(cmdline), "\x00", " "))}, nil
}

// hasOtherThreads: the leader of a threaded process is a zombie as soon as it has exited, while its other threads (and so its open
// sockets) are still being torn down; such a process is not gone yet.
func hasOtherThreads(dir string, pid int) bool {
	tasks, err := os.ReadDir(dir + "/task")
	if err != nil {
		return false
	}
	for _, t := range tasks {
		if t.Name() != strconv.Itoa(pid) {
			return true
		}
	}
	return false
}
