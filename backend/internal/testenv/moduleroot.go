package testenv

import (
	"os"
	"path/filepath"
	"testing"
)

// ModuleRoot is the directory of go.mod above the test's working directory (go test runs a test in its package
// directory). A fixture outside the package (a file of the repository root, a testdata of another package) is found
// from there, not from runtime.Caller: under -trimpath that is a module path, not a path on disk.
func ModuleRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above the working directory")
		}
		dir = parent
	}
}
