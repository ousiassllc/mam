package genapprovalrepo_test

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

// beforeNoApproval is the migration before 20261111100000_generation_launch_without_approval.sql.
const beforeNoApproval = 20261110100400

// TestMigration_GenerationLaunchLosesItsApprovalAndKeepsItsClaims: a claim of v0.1.34 (it pointed at an approval card) survives the rebuild of
// the table with its job and outcome, a claim is made without a card from then on, and the primary key still keeps a stage from being claimed twice.
// The records of the cards (generation_approval) stay, to be read.
func TestMigration_GenerationLaunchLosesItsApprovalAndKeepsItsClaims(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, beforeNoApproval)
	require.NoError(t, err)
	for _, q := range []string{
		`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p', 'auto', 'full_auto', 'product_cm', 'balanced')`,
		`INSERT INTO scene (id, project_id, order_index) VALUES ('sc', 'p', 0)`,
		`INSERT INTO shot (id, scene_id, order_index) VALUES ('s1', 'sc', 0), ('s2', 'sc', 1)`,
		`INSERT INTO chat_message (id, project_id, role, kind, ref_type, ref_id, approval_status) VALUES ('m1', 'p', 'assistant', 'approval', 'generation', 'fp', 'approved')`,
		`INSERT INTO generation_approval (message_id, project_id, fingerprint, scope) VALUES ('m1', 'p', 'fp', '[]')`,
		`INSERT INTO generation_launch (shot_id, stage, message_id, job_id, outcome) VALUES ('s1', 'reference_pack', 'm1', 'j1', 'started'), ('s2', 'generate', 'm1', NULL, 'budget')`,
	} {
		_, err := db.ExecContext(ctx, q)
		require.NoError(t, err, q)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO generation_launch (shot_id, stage) VALUES ('s1', 'generate')`)
	require.Error(t, err, "before the migration a claim needs its approval card")

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	var jobID sql.NullString
	var outcome string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT job_id, outcome FROM generation_launch WHERE shot_id = 's1' AND stage = 'reference_pack'`).Scan(&jobID, &outcome))
	assert.Equal(t, "j1", jobID.String)
	assert.Equal(t, "started", outcome)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT outcome FROM generation_launch WHERE shot_id = 's2' AND stage = 'generate'`).Scan(&outcome))
	assert.Equal(t, "budget", outcome, "a stage that was refused stays refused")
	_, err = db.ExecContext(ctx, `INSERT INTO generation_launch (shot_id, stage) VALUES ('s1', 'generate')`)
	require.NoError(t, err, "a claim needs no approval card now")
	_, err = db.ExecContext(ctx, `INSERT INTO generation_launch (shot_id, stage) VALUES ('s1', 'generate')`)
	assert.Error(t, err, "a stage is claimed once")
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM generation_approval`).Scan(&n))
	assert.Equal(t, 1, n, "the records of the old cards are kept")

	_, err = db.ExecContext(ctx, `DELETE FROM shot WHERE id = 's2'`)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM generation_launch WHERE shot_id = 's2'`).Scan(&n))
	assert.Zero(t, n, "a claim goes with its Shot")

	_, err = provider.DownTo(ctx, beforeNoApproval)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `SELECT message_id FROM generation_launch`)
	assert.NoError(t, err, "going down restores the column")
}
