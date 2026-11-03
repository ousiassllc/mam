package characterrepo_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// beforeDraft is the migration before 20261106100000_character_draft.sql.
const beforeDraft = 20261105090000

// TestMigration_CharacterDraftMarksNoExistingCharacterAndGoesBack: the Characters that exist before the column are not drafts, the column
// takes only 0 and 1, and going down keeps the rows without it.
func TestMigration_CharacterDraftMarksNoExistingCharacterAndGoesBack(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, beforeDraft)
	require.NoError(t, err)
	for _, q := range []string{
		`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p', 'auto', 'key_check', 'product_cm', 'balanced')`,
		`INSERT INTO character (id, project_id, name) VALUES ('c1', 'p', 'Mio')`,
	} {
		_, err := db.ExecContext(ctx, q)
		require.NoError(t, err, q)
	}
	_, err = db.ExecContext(ctx, `SELECT draft FROM character`)
	require.Error(t, err, "there is no column before the migration")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	var draft int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT draft FROM character WHERE id = 'c1'`).Scan(&draft))
	assert.Zero(t, draft, "a Character that was there is no draft")
	_, err = db.ExecContext(ctx, `INSERT INTO character (id, project_id, name, draft) VALUES ('c2', 'p', 'Ren', 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO character (id, project_id, name, draft) VALUES ('c3', 'p', 'Kai', 2)`)
	assert.Error(t, err, "the column takes 0 and 1 only")

	_, err = provider.DownTo(ctx, beforeDraft)
	require.NoError(t, err)
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM character`).Scan(&n))
	assert.Equal(t, 2, n, "going down keeps the Characters")
	_, err = db.ExecContext(ctx, `SELECT draft FROM character`)
	assert.Error(t, err, "and drops the column")
}
