package export_test

import (
	"errors"
	"os"
	"time"
)

func jst() *time.Location { return time.FixedZone("JST", 9*60*60) }

func removeFile(path string) error { return os.Remove(path) }

// failAfter is a writer that breaks after n bytes.
type failAfter struct{ n int }

func (f failAfter) Write(p []byte) (int, error) {
	if len(p) > f.n {
		return f.n, errors.New("connection closed")
	}
	return len(p), nil
}
