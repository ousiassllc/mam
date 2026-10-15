package fsutil_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

func TestStageReaderWritesTheBodyToANewFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", ".staging")

	path, n, err := fsutil.StageReader(dir, strings.NewReader("MP4DATA"), 100)

	if err != nil || n != 7 {
		t.Fatalf("StageReader = %q, %d, %v", path, n, err)
	}
	if got, _ := os.ReadFile(path); string(got) != "MP4DATA" || filepath.Dir(path) != dir {
		t.Fatalf("staged %q in %q", got, path)
	}
}

func TestStageReaderTellsDiskFailuresFromBodyFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	staging := t.TempDir()
	tests := []struct {
		name      string
		dir       string
		body      io.Reader
		wantLocal bool
		want      error // nil: any error
	}{
		{"directory is a file", filepath.Join(file, "x"), strings.NewReader("data"), true, nil},
		{"body fails", staging, iotest.ErrReader(io.ErrUnexpectedEOF), false, io.ErrUnexpectedEOF},
		{"body too large", staging, strings.NewReader("0123456789"), false, fsutil.ErrTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := fsutil.StageReader(tt.dir, tt.body, 5)

			var local *fsutil.LocalError
			if err == nil || errors.As(err, &local) != tt.wantLocal || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("err = %v, want local %v / %v", err, tt.wantLocal, tt.want)
			}
			if left, _ := filepath.Glob(filepath.Join(staging, "*")); len(left) != 0 {
				t.Fatalf("staged files left: %v", left)
			}
		})
	}
}

func TestLocalErrorKeepsTheCause(t *testing.T) {
	err := error(&fsutil.LocalError{Err: syscall.ENOSPC})
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatal("the cause of a LocalError must stay reachable")
	}
}
