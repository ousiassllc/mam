package bgwork

import (
	"log/slog"
	"runtime/debug"
)

// RunSafely runs fn and turns a panic in it into a log line with its stack. Use it for work that runs
// on a goroutine of its own, outside the HTTP middleware's and the job queue's recover: a panic there
// would end the whole server (and the running, already paid generation Jobs with it).
func RunSafely(logger *slog.Logger, name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(name+" panicked", "panic", r, "stack", string(debug.Stack()))
		}
	}()
	fn()
}

// Async returns fn as a callback that runs on a goroutine of its own (RunSafely), for a callback that
// must not block its caller. It does not wait for the runs.
func Async[T any](logger *slog.Logger, name string, fn func(T)) func(T) {
	return func(v T) { go RunSafely(logger, name, func() { fn(v) }) }
}
