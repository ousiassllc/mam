// Package testdb gives tests a freshly migrated SQLite database without paying for the
// migrations on every test.
//
// The schema is migrated once per test binary into an in-memory template; each test then gets its
// own copy of that file. Copying takes a few milliseconds, applying every migration takes over
// 0.1 s, and the repository / service / agent packages open a database in almost every test.
package testdb

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

var (
	templateOnce sync.Once
	template     []byte
	templateErr  error
)

// Path returns the path of a private, fully migrated database file inside t.TempDir(). Nothing holds
// it open: use it for tests that open the file themselves (repository.Open, a config's DBPath).
func Path(t testing.TB) string {
	t.Helper()
	templateOnce.Do(func() { template, templateErr = migratedTemplate() })
	require.NoError(t, templateErr)
	path := filepath.Join(t.TempDir(), "mam.db")
	require.NoError(t, os.WriteFile(path, template, 0o600))
	return path
}

// Open opens a private, fully migrated database and closes it when the test ends.
func Open(t testing.TB) *repository.DB {
	t.Helper()
	db, err := repository.Open(context.Background(), Path(t), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// migratedTemplate migrates a scratch database and returns its bytes. Close checkpoints and removes
// the WAL, so the main file alone holds the whole schema.
func migratedTemplate() ([]byte, error) {
	dir, err := os.MkdirTemp("", "mam-testdb-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "template.db")
	db, err := repository.Open(context.Background(), path, nil)
	if err != nil {
		return nil, err
	}
	if err := db.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path) // #nosec G304 -- a file under the scratch directory created above
}
