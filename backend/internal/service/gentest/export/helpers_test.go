package export_test

import (
	"errors"
	"os"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/export"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
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

func ids(list []record.ResultView) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = v.ID
	}
	return out
}

func idsOf(items []export.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ResultID
	}
	return out
}
