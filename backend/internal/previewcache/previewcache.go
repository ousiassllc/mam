// Package previewcache makes the preview files of Takes (a 1080p proxy of a 4K Take, a thumbnail)
// once, however often a browser asks for them. A <video> asks again at every seek (a Range
// request) and cancels what it asked for when the card is replaced, so without this every request
// would redo the ffprobe and ffmpeg runs, which take minutes on a file that cannot be converted.
package previewcache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/bgwork"
	"github.com/ousiassllc/mam/backend/internal/keylock"
)

// ErrFailedRecently means a file was not made because making it failed a moment ago; the error of
// that failure is wrapped.
var ErrFailedRecently = errors.New("previewcache: failed recently")

var errPanicked = errors.New("previewcache: making the file panicked")

// Cache is the memory of the preview files that are being made, that failed, or whose source is known
// to be tall. It lives as long as the service that owns it.
type Cache struct {
	logger *slog.Logger
	// lock serializes the making of one file: two requests for it wait for one ffmpeg run.
	lock keylock.Locker // file path
	// tall is the answer of Tall (id -> bool).
	tall sync.Map
	// failed is the last failure of making a file (file path -> failure).
	failed sync.Map
}

type failure struct {
	at  time.Time
	err error
}

// New returns a Cache; logger tells a panic while a file is made.
func New(logger *slog.Logger) *Cache { return &Cache{logger: logger} }

// Tall returns the answer of probe for id, asking it once: the file behind an id never changes. A
// probe that fails is not remembered.
func (c *Cache) Tall(id string, probe func() (bool, error)) (bool, error) {
	if v, ok := c.tall.Load(id); ok {
		return v.(bool), nil
	}
	tall, err := probe()
	if err == nil {
		c.tall.Store(id, tall)
	}
	return tall, err
}

// Make runs build to make the file dst, unless dst exists already. Concurrent calls for one file wait
// for the first and stop waiting when ctx ends.
//
// build is not bound to ctx: a client that gives up (a seek, a page change) must not kill a
// conversion that takes minutes and make the next request start over. build's own time limit ends a
// hung one, and dst stays locked until build returns, whether or not the caller still waits.
//
// When build fails and retryAfter is positive, calls for dst in the next retryAfter return
// ErrFailedRecently (wrapping the error of build) without running build again.
func (c *Cache) Make(ctx context.Context, dst string, retryAfter time.Duration, build func(ctx context.Context) error) error {
	unlock, err := c.lock.Lock(ctx, dst)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		unlock()
		return nil
	}
	if f, ok := c.failed.Load(dst); ok && time.Since(f.(failure).at) < retryAfter {
		unlock()
		return fmt.Errorf("%w: %w", ErrFailedRecently, f.(failure).err)
	}
	done := make(chan error, 1)
	go bgwork.RunSafely(c.logger, "make preview", func() {
		err := errPanicked
		defer func() { unlock(); done <- err }() // also when build panics
		err = build(context.WithoutCancel(ctx))
		switch {
		case err == nil:
			c.failed.Delete(dst)
		case retryAfter > 0:
			c.failed.Store(dst, failure{at: time.Now(), err: err})
		}
	})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
