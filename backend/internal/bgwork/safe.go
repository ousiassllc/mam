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
