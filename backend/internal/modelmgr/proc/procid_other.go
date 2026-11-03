//go:build !linux && !darwin && !windows

package proc

// probeProcess: no way to tell processes apart on the other systems.
func probeProcess(int) (processID, error) { return processID{}, errUnsupported }
