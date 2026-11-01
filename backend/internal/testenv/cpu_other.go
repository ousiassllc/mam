//go:build !linux

package testenv

import "time"

var epoch = time.Now()

// threadCPUTime is the wall clock: only Linux has the CPU time of a thread without cgo.
func threadCPUTime() time.Duration { return time.Since(epoch) }
