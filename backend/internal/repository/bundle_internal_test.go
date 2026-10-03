package repository

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
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

// TestBundle_NoIDMarksExactlyTheTablesWithoutAnIDColumn keeps checkBundle's "every row has an id"
// rule honest: a bundled table whose primary key is not "id" must say so.
func TestBundle_NoIDMarksExactlyTheTablesWithoutAnIDColumn(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	for _, b := range bundleTables {
		var hasID int
		require.NoError(t, db.Reader().QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name = 'id' AND pk = 1`, b.name).Scan(&hasID))
		assert.Equal(t, hasID == 0, b.noID, "table %s", b.name)
	}
}

// TestBundle_EveryJSONColumnHasAShapeOrIsKeptRaw makes a new json_valid column of a bundled table
// fail here until its author says what the reader decodes it into (bundleJSONColumns) or that any
// JSON will do (rawJSONColumns): checkBundle refuses a bundle row the readers cannot decode.
func TestBundle_EveryJSONColumnHasAShapeOrIsKeptRaw(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	jsonColumn := regexp.MustCompile(`json_valid\((\w+)\)`)
	for _, b := range bundleTables {
		var ddl string
		require.NoError(t, db.Reader().QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, b.name).Scan(&ddl))
		for _, m := range jsonColumn.FindAllStringSubmatch(ddl, -1) {
			_, shaped := bundleJSONColumns[b.name][m[1]]
			assert.True(t, shaped || slices.Contains(rawJSONColumns[b.name], m[1]), "%s.%s is neither in bundleJSONColumns nor in rawJSONColumns", b.name, m[1])
		}
	}
	for table, columns := range bundleJSONColumns {
		for column := range columns {
			assert.NotContains(t, rawJSONColumns[table], column, "%s.%s is in both lists", table, column)
		}
	}
}
