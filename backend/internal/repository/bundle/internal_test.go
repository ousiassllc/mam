package bundle

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// TestBundle_EveryTableIsBundledOrHasAReasonNotToBe makes a new table fail here until its author
// decides whether the Project bundle carries it: a table silently missing from bundleTables would
// make exported Projects lose data.
func TestBundle_EveryTableIsBundledOrHasAReasonNotToBe(t *testing.T) {
	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
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
	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
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
	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
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

// TestBundle_EveryNumericColumnRefusesText makes the type check follow the schema: a text value in
// any INTEGER, REAL or NUMERIC column of a bundled table (which SQLite would store as it is, and
// the readers fail to Scan) is refused, and a number is not. A new numeric column is covered
// without being listed.
func TestBundle_EveryNumericColumnRefusesText(t *testing.T) {
	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	numeric := 0
	for _, b := range bundleTables {
		rows, err := db.Reader().Query(`SELECT name, type FROM pragma_table_info(?)`, b.name)
		require.NoError(t, err)
		declared := map[string]string{}
		for rows.Next() {
			var name, typ string
			require.NoError(t, rows.Scan(&name, &typ))
			declared[name] = typ
		}
		require.NoError(t, rows.Close())
		for column, typ := range declared {
			if classOf(typ) == anyClass {
				continue
			}
			numeric++
			for _, bad := range []any{"x", "1", true, json.Number("1e999")} {
				assert.ErrorIs(t, checkBundleColumnTypes(b.name, declared, domain.BundleRow{column: bad}), ErrInvalid, "%s.%s (%s) = %#v", b.name, column, typ, bad)
			}
			fraction := checkBundleColumnTypes(b.name, declared, domain.BundleRow{column: json.Number("1.5")})
			if classOf(typ) == integerClass {
				assert.ErrorIs(t, fraction, ErrInvalid, "%s.%s", b.name, column)
			} else {
				assert.NoError(t, fraction, "%s.%s", b.name, column)
			}
			assert.NoError(t, checkBundleColumnTypes(b.name, declared, domain.BundleRow{column: json.Number("2")}), "%s.%s", b.name, column)
			assert.NoError(t, checkBundleColumnTypes(b.name, declared, domain.BundleRow{column: nil}), "%s.%s", b.name, column)
		}
	}
	assert.Positive(t, numeric)
}

// TestInsertBundleTable_OnlyTheBundlesFaultIsInvalidBundle: a row the schema refuses is an invalid
// bundle; a failure of the database itself (here a cancelled context) is not, so that the caller
// does not tell the user their data is unreadable.
func TestInsertBundleTable_OnlyTheBundlesFaultIsInvalidBundle(t *testing.T) {
	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	insert := func(ctx context.Context, table domain.BundleTable) error {
		return db.Write(context.Background(), func(_ context.Context, tx *sql.Tx) error {
			return insertBundleTable(ctx, tx, table)
		})
	}
	// project.mode is CHECKed: the schema refuses the row.
	refused := domain.BundleTable{Name: "project", Rows: []domain.BundleRow{{"id": "p", "mode": "bogus"}}}
	assert.ErrorIs(t, insert(context.Background(), refused), ErrInvalid)
	assert.ErrorIs(t, insert(context.Background(), domain.BundleTable{Name: "project", Rows: []domain.BundleRow{{"nope": "x"}}}), ErrInvalid)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	err = insert(cancelled, domain.BundleTable{Name: "project", Rows: []domain.BundleRow{{"id": "p", "mode": "auto"}}})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalid)
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, insertError("project", context.Canceled), ErrInvalid)
}
