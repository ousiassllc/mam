// Package fanout runs the independent parts of a generation (the Takes of a Shot, the images of a Reference Pack) at the same time.
package fanout

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
)

// Run runs fn for i in [0, n) concurrently and returns the errors in order. A panic in fn is
// not propagated (it would end the goroutine, and the whole server, outside the job queue's
// recover): it is logged with its stack and becomes the error of that index, while the others
// finish.
func Run(logger *slog.Logger, n int, fn func(i int) error) []error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("parallel task panicked", "index", i, "panic", r, "stack", string(debug.Stack()))
					errs[i] = fmt.Errorf("task %d panicked: %v", i, r)
				}
			}()
			errs[i] = fn(i)
		})
	}
	wg.Wait()
	return errs
}

// Counter counts finished Takes from concurrent goroutines and remembers the first one.
type Counter struct {
	mu      sync.Mutex
	n       int
	firstID string
}

// Add counts a finished Take and returns how many have finished.
func (c *Counter) Add(takeID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == 0 {
		c.firstID = takeID
	}
	c.n++
	return c.n
}

// Value is how many Takes have finished.
func (c *Counter) Value() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// First is the ID of the Take that was finished first ("" when none).
func (c *Counter) First() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.firstID
}
