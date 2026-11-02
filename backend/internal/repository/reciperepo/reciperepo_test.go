package reciperepo_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/reciperepo"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// beforeRecipe is the migration before 20261104100000_gentest_recipe.sql.
const beforeRecipe = 20261103200000

func TestMigration_GentestRecipeUpAndDown(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	tables := func() (n int) {
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = 'gentest_recipe'`).Scan(&n))
		return n
	}

	_, err = provider.UpTo(ctx, beforeRecipe)
	require.NoError(t, err)
	assert.Zero(t, tables())
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, tables())

	exec := func(q string, args ...any) error { _, err := db.ExecContext(ctx, q, args...); return err }
	require.NoError(t, exec(`INSERT INTO gentest_recipe (id, name, params) VALUES ('a', 'Portrait', '{}')`))
	assert.Error(t, exec(`INSERT INTO gentest_recipe (id, name, params) VALUES ('b', 'portrait', '{}')`), "names are unique ignoring case")
	assert.Error(t, exec(`INSERT INTO gentest_recipe (id, name, params) VALUES ('c', 'x', 'not json')`), "params is JSON")
	assert.Error(t, exec(`INSERT INTO gentest_recipe (id, name, params, easy) VALUES ('d', 'y', '{}', 2)`), "easy is 0 or 1")
	assert.Error(t, exec(`INSERT INTO gentest_recipe (id, name, params) VALUES ('e', '', '{}')`), "a name is required")

	_, err = provider.DownTo(ctx, beforeRecipe)
	require.NoError(t, err)
	assert.Zero(t, tables())
}

func sample(id, name string, at time.Time) reciperepo.Recipe {
	return reciperepo.Recipe{ID: id, Name: name, Params: json.RawMessage(`{"model":"m"}`), CreatedAt: at, UpdatedAt: at}
}

func TestRepository_CreateListRenameDelete(t *testing.T) {
	ctx := context.Background()
	repo := reciperepo.New(testdb.Open(t))
	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

	require.NoError(t, repo.Create(ctx, sample("r1", "Zebra", at)))
	require.NoError(t, repo.Create(ctx, sample("r2", "apple", at)))
	assert.ErrorIs(t, repo.Create(ctx, sample("r3", "ZEBRA", at)), repository.ErrConflict)

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, []string{"apple", "Zebra"}, []string{list[0].Name, list[1].Name}, "by name, ignoring case")

	got, err := repo.Get(ctx, "r1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"m"}`, string(got.Params))
	assert.False(t, got.Easy)
	assert.True(t, at.Equal(got.CreatedAt))

	got.Name, got.Easy, got.UpdatedAt = "Quagga", true, at.Add(time.Hour)
	require.NoError(t, repo.Update(ctx, got))
	got, err = repo.Get(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, "Quagga", got.Name)
	assert.True(t, got.Easy)
	assert.JSONEq(t, `{"model":"m"}`, string(got.Params), "the params are kept")

	taken := got
	taken.Name = "APPLE"
	assert.ErrorIs(t, repo.Update(ctx, taken), repository.ErrConflict)
	assert.ErrorIs(t, repo.Update(ctx, sample("nope", "x", at)), repository.ErrNotFound)

	require.NoError(t, repo.Delete(ctx, "r1"))
	assert.ErrorIs(t, repo.Delete(ctx, "r1"), repository.ErrNotFound)
	_, err = repo.Get(ctx, "r1")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}
