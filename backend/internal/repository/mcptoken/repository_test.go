package mcptoken_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/mcptoken"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// beforeMCPToken is the migration before 20261024120000_mcp_token.sql.
const beforeMCPToken = 20261002120000

func TestMigration_McpTokenUpAndDown(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	tables := func() int {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = 'mcp_token'`).Scan(&n))
		return n
	}

	_, err = provider.UpTo(ctx, beforeMCPToken)
	require.NoError(t, err)
	assert.Zero(t, tables())

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, tables())
	_, err = db.ExecContext(ctx, `INSERT INTO mcp_token (id, name, token_hash, token_prefix) VALUES ('1', 'a', 'h', 'p')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO mcp_token (id, name, token_hash, token_prefix) VALUES ('2', 'b', 'h', 'p')`)
	assert.Error(t, err, "two tokens cannot share a hash")
	_, err = db.ExecContext(ctx, `INSERT INTO mcp_token (id, name, token_hash, token_prefix) VALUES ('3', '', 'h3', 'p')`)
	assert.Error(t, err, "a name is required")

	_, err = provider.DownTo(ctx, beforeMCPToken)
	require.NoError(t, err)
	assert.Zero(t, tables())
}

func TestRepository_RoundTrip(t *testing.T) {
	ctx := context.Background()
	r := mcptoken.New(testdb.Open(t))
	created := time.Date(2026, 10, 24, 1, 2, 3, 0, time.UTC)

	require.NoError(t, r.Create(ctx, mcptoken.Token{ID: "a", Name: "first", Prefix: "AAAAAAAA", CreatedAt: created}, "hash-a"))
	require.NoError(t, r.Create(ctx, mcptoken.Token{ID: "b", Name: "second", Prefix: "BBBBBBBB", CreatedAt: created.Add(time.Minute)}, "hash-b"))

	got, err := r.GetByHash(ctx, "hash-a")
	require.NoError(t, err)
	assert.Equal(t, "first", got.Name)
	assert.True(t, got.CreatedAt.Equal(created))
	assert.Nil(t, got.LastUsedAt)
	assert.False(t, got.Revoked())
	_, err = r.GetByHash(ctx, "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)

	list, err := r.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "b", list[0].ID, "newest first")

	used := created.Add(time.Hour)
	require.NoError(t, r.TouchUsed(ctx, "a", used))
	revoked := created.Add(2 * time.Hour)
	require.NoError(t, r.Revoke(ctx, "a", revoked))
	require.NoError(t, r.Revoke(ctx, "a", revoked.Add(time.Hour)))
	got, err = r.Get(ctx, "a")
	require.NoError(t, err)
	assert.True(t, got.LastUsedAt.Equal(used))
	assert.True(t, got.RevokedAt.Equal(revoked))
	assert.ErrorIs(t, r.Revoke(ctx, "missing", revoked), repository.ErrNotFound)
}
