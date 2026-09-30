package repository_test

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

// beforeBranch is the migration before 20260930030000_branch.sql.
const beforeBranch = 20260930020000

// TestMigration_BranchKeepsWorldStateVersionsAndTheirDependents rebuilds world_state_version
// on a database that already holds data: the versions and the state_diff rows that point at them
// must survive (dropping the table with foreign keys on would cascade into state_diff), and the
// migration must be reversible.
func TestMigration_BranchKeepsWorldStateVersionsAndTheirDependents(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, beforeBranch)
	require.NoError(t, err)
	for _, q := range []string{
		`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p', 'auto', 'key_check', 'product_cm', 'balanced')`,
		`INSERT INTO scene (id, project_id, order_index) VALUES ('sc', 'p', 0)`,
		`INSERT INTO shot (id, scene_id, order_index) VALUES ('sh', 'sc', 0)`,
		`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv', 'sh', 1, '{}')`,
		`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, status) VALUES ('t', 'pv', 'kling-3.0', '{}', 'candidate')`,
		`INSERT INTO world_state_version (id, project_id, version_number, state) VALUES ('w1', 'p', 1, '{"a":1}'), ('w2', 'p', 2, '{"a":2}')`,
		`INSERT INTO state_diff (id, shot_id, take_id, diff, world_state_version_id) VALUES ('d', 'sh', 't', '{}', 'w2')`,
	} {
		_, err := db.ExecContext(ctx, q)
		require.NoError(t, err, q)
	}

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	count := func(q string) int {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, q).Scan(&n))
		return n
	}
	assert.Equal(t, 2, count(`SELECT COUNT(*) FROM world_state_version WHERE branch_id IS NULL`), "existing versions become the trunk series")
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM state_diff WHERE world_state_version_id = 'w2'`), "the dependent row was not cascaded away")
	assert.Equal(t, 1, count(`PRAGMA foreign_keys`), "foreign keys are back on")
	assert.Zero(t, count(`SELECT COUNT(*) FROM pragma_foreign_key_check`))

	_, err = provider.DownTo(ctx, beforeBranch)
	require.NoError(t, err)
	assert.Equal(t, 2, count(`SELECT COUNT(*) FROM world_state_version`))
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM state_diff`))
	assert.Zero(t, count(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('branch', 'branch_version')`))
}
