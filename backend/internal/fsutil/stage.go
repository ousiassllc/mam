package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrTooLarge means the body handed to StageReader is longer than the limit.
var ErrTooLarge = errors.New("the body is larger than the limit")

// LocalError is a failure of this machine's disk (creating the directory or the file, writing,
// syncing, closing), as opposed to a failure of reading the body, which may come from the network.
// Repeating the transfer fails the same way, so a caller that retries network failures must not
// retry a LocalError.
type LocalError struct{ Err error }

func (e *LocalError) Error() string { return e.Err.Error() }
func (e *LocalError) Unwrap() error { return e.Err }

// localWriter marks the errors of w as LocalError, so that a failing write is told apart from a
// failing read of the body when both come out of one io.Copy.
type localWriter struct{ w io.Writer }

func (l localWriter) Write(p []byte) (int, error) {
	n, err := l.w.Write(p)
	if err != nil {
		err = &LocalError{Err: err}
	}
	return n, err
}

// StageReader streams at most limit bytes of body into a new file of dir (CreateStagingTemp), syncs
// it and returns its path and size. The file is removed again when it cannot be completed. The
// errors are ErrTooLarge, a *LocalError for a failure of the disk, and the error of body as it is.
func StageReader(dir string, body io.Reader, limit int64) (path string, n int64, err error) {
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", 0, &LocalError{Err: fmt.Errorf("create staging directory: %w", err)}
		}
	}
	f, err := CreateStagingTemp(dir)
	if err != nil {
		return "", 0, &LocalError{Err: fmt.Errorf("create staging file: %w", err)}
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	n, err = io.Copy(localWriter{f}, io.LimitReader(body, limit+1))
	if err != nil {
		return "", 0, err
	}
	if n > limit {
		return "", 0, ErrTooLarge
	}
	if err = f.Sync(); err != nil {
		return "", 0, &LocalError{Err: fmt.Errorf("sync staging file: %w", err)}
	}
	if err = f.Close(); err != nil {
		return "", 0, &LocalError{Err: fmt.Errorf("close staging file: %w", err)}
	}
	return f.Name(), n, nil
}
