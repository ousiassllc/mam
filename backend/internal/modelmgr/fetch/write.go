package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

const (
	defaultIdle = 60 * time.Second
	copyChunk   = 1 << 20
)

func dirOf(path string) string { return filepath.Dir(path) }

// write appends the body to part (truncated to its first have bytes, which are hashed again so that the
// SHA-256 covers the whole file), then checks the size and the SHA-256. A stalled body is cut after the idle
// timeout.
func write(ctx context.Context, cancel context.CancelFunc, part string, have int64, body io.Reader, src Source, o Options) (err error) {
	f, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- a path the model manager chose
	if err != nil {
		return fmt.Errorf("fetch: open %s: %w", part, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if err := f.Truncate(have); err != nil {
		return err
	}
	h := sha256.New()
	if have > 0 {
		if _, err := io.Copy(h, io.NewSectionReader(f, 0, have)); err != nil {
			return err
		}
	}
	if _, err := f.Seek(have, io.SeekStart); err != nil {
		return err
	}
	idle := newWatchdog(cancel, cmpDuration(o.IdleTimeout, defaultIdle))
	defer idle.stop()
	done := have
	buf := make([]byte, copyChunk)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			idle.touch()
			if done+int64(n) > src.Size {
				return ErrSize
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return localErr(err)
			}
			h.Write(buf[:n])
			done += int64(n)
			report(o, done, src.Size)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			return rerr
		}
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if done != src.Size {
		if done < src.Size { // the connection ended early: keep the part file and resume later
			return io.ErrUnexpectedEOF
		}
		return ErrSize
	}
	if err := f.Sync(); err != nil {
		return localErr(err)
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), src.SHA256) {
		return ErrChecksum
	}
	return nil
}

// localErr marks a failure of the local disk.
func localErr(err error) error { return &fsutil.LocalError{Err: err} }

func cmpDuration(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// watchdog cancels a transfer that delivers nothing for d.
type watchdog struct {
	mu sync.Mutex
	t  *time.Timer
	d  time.Duration
}

func newWatchdog(cancel context.CancelFunc, d time.Duration) *watchdog {
	return &watchdog{d: d, t: time.AfterFunc(d, cancel)}
}

func (w *watchdog) touch() {
	w.mu.Lock()
	w.t.Reset(w.d)
	w.mu.Unlock()
}

func (w *watchdog) stop() {
	w.mu.Lock()
	w.t.Stop()
	w.mu.Unlock()
}
