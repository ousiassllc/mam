package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBundle_EveryTableIsBundledOrHasAReasonNotToBe makes a new table fail here until its author
// decides whether the Project bundle carries it: a table silently missing from bundleTables would
// make exported Projects lose data.
func TestBundle_EveryTableIsBundledOrHasAReasonNotToBe(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	rows, err := db.Reader().Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	bundled := map[string]bool{}
	for _, b := range bundleTables {
		bundled[b.name] = true
	}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		_, skipped := unbundledTables[name]
		assert.True(t, bundled[name] || skipped, "table %s is neither in bundleTables nor in unbundledTables", name)
		delete(bundled, name)
	}
	assert.Empty(t, bundled, "bundleTables names tables that do not exist")
}
