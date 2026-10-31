package jobhistory_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobhistory"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// beforeJobHistory is the migration before 20261031120000_mcp_job_history.sql.
const beforeJobHistory = 20261025150000

func TestMigration_McpJobHistoryUpAndDown(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	objects := func() int {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name LIKE '%mcp_job_history%'`).Scan(&n))
		return n
	}

	_, err = provider.UpTo(ctx, beforeJobHistory)
	require.NoError(t, err)
	assert.Zero(t, objects())

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 4, objects(), "the table, the index of its primary key and its two indexes")
	_, err = db.ExecContext(ctx, `INSERT INTO mcp_job_history (id, kind, state, started_at) VALUES ('a', 'k', 'running', '2026-10-31T00:00:00.000Z')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO mcp_job_history (id, kind, state, started_at) VALUES ('b', 'k', 'exploded', '2026-10-31T00:00:00.000Z')`)
	assert.Error(t, err, "an unknown state is refused")
	_, err = db.ExecContext(ctx, `INSERT INTO mcp_job_history (id, kind, state, started_at, result) VALUES ('c', 'k', 'running', '2026-10-31T00:00:00.000Z', 'not json')`)
	assert.Error(t, err, "a result is JSON")

	_, err = provider.DownTo(ctx, beforeJobHistory)
	require.NoError(t, err)
	assert.Zero(t, objects())
}

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func entry(id string, started time.Time) jobhistory.Entry {
	return jobhistory.Entry{ID: id, Kind: "judge_take", Target: "t-" + id, StartedAt: started, State: "queued"}
}

func TestRepository_RoundTripAndSettle(t *testing.T) {
	ctx := context.Background()
	r := jobhistory.New(testdb.Open(t))
	e := entry("job_1", t0)
	e.QueueJobID = "q1"
	require.NoError(t, r.Insert(ctx, e))

	got, err := r.Get(ctx, "job_1")
	require.NoError(t, err)
	assert.Equal(t, e, got)
	assert.True(t, got.Open())
	_, err = r.Get(ctx, "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	again := e
	again.Target, again.StartedAt = "other", t0.Add(time.Hour)
	require.NoError(t, r.Insert(ctx, again), "the same queue job answered twice is not an error")
	got, err = r.Get(ctx, "job_1")
	require.NoError(t, err)
	assert.Equal(t, e, got, "the first row stays")

	finished := t0.Add(time.Minute)
	e.State, e.Message, e.Done, e.Total = "failed", "done with it", 1, 2
	e.ErrorCode, e.ErrorMessage, e.ErrorID = "internal", "internal: boom", "err-1"
	e.Result = json.RawMessage(`{"take_ids":["a"]}`)
	e.FinishedAt, e.EndedAt = &finished, &finished
	require.NoError(t, r.Settle(ctx, e))
	got, err = r.Get(ctx, "job_1")
	require.NoError(t, err)
	assert.Equal(t, e.State, got.State)
	assert.Equal(t, e.ErrorMessage, got.ErrorMessage)
	assert.Equal(t, "err-1", got.ErrorID)
	assert.JSONEq(t, `{"take_ids":["a"]}`, string(got.Result))
	assert.True(t, finished.Equal(*got.FinishedAt))
	assert.False(t, got.Open())

	assert.ErrorIs(t, r.Settle(ctx, entry("ghost", t0)), repository.ErrNotFound)
}

func TestRepository_OpenListsTheUnfinishedNewestFirst(t *testing.T) {
	ctx := context.Background()
	r := jobhistory.New(testdb.Open(t))
	for i, state := range []string{"queued", "succeeded", "running"} {
		e := entry(fmt.Sprintf("j%d", i), t0.Add(time.Duration(i)*time.Minute))
		e.State = state
		require.NoError(t, r.Insert(ctx, e))
	}
	open, err := r.Open(ctx, 10)
	require.NoError(t, err)
	require.Len(t, open, 2)
	assert.Equal(t, []string{"j2", "j0"}, []string{open[0].ID, open[1].ID})
	open, err = r.Open(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, open, 1)
}

func TestRepository_InsertFindsTheProjectOfTheTarget(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
			`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 4, 0)`,
			`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{"shot_objective":"run"}')`,
			`INSERT INTO take (id, prompt_version_id, model_name) VALUES ('t1', 'pv1', 'kling-3.0')`,
			`INSERT INTO edit_timeline_version (id, project_id, version_number, timeline) VALUES ('e1', 'p1', 1, '{}')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	r := jobhistory.New(db)
	for target, want := range map[string]string{"p1": "p1", "sh1": "p1", "t1": "p1", "e1": "p1", "a-model-id": "", "": ""} {
		e := entry("job_"+target, t0)
		e.Target = target
		require.NoError(t, r.Insert(ctx, e))
		got, err := r.Get(ctx, e.ID)
		require.NoError(t, err)
		assert.Equal(t, want, got.ProjectID, "target %q", target)
	}
	// A Project that is deleted later does not take the history with it.
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM project WHERE id = 'p1'`)
		return err
	}))
	got, err := r.Get(ctx, "job_sh1")
	require.NoError(t, err)
	assert.Equal(t, "p1", got.ProjectID)
}

func ids(t *testing.T, r *jobhistory.Repository) []string {
	t.Helper()
	rows, err := r.Open(context.Background(), 100) // every row below is open
	require.NoError(t, err)
	out := make([]string, len(rows))
	for i, e := range rows {
		out[i] = e.ID
	}
	return out
}

func TestRepository_PrunesByAgeWhenARowIsAddedAndAtStartup(t *testing.T) {
	ctx := context.Background()
	now := t0
	r := jobhistory.New(testdb.Open(t)).WithClock(func() time.Time { return now })
	require.NoError(t, r.Insert(ctx, entry("old", t0.Add(-jobhistory.Retention+time.Hour))))
	require.NoError(t, r.Insert(ctx, entry("new", t0)))
	assert.ElementsMatch(t, []string{"old", "new"}, ids(t, r))

	now = t0.Add(2 * time.Hour) // "old" is now past 30 days
	require.NoError(t, r.Prune(ctx), "the startup prune")
	assert.Equal(t, []string{"new"}, ids(t, r))

	now = t0.Add(jobhistory.Retention + time.Hour)
	require.NoError(t, r.Insert(ctx, entry("newest", now)), "adding a row prunes too")
	assert.Equal(t, []string{"newest"}, ids(t, r))
}

func TestRepository_KeepsTheNewestRowsUpToTheCap(t *testing.T) {
	ctx := context.Background()
	r := jobhistory.New(testdb.Open(t)).WithClock(func() time.Time { return t0 }).WithLimits(jobhistory.Retention, 3)
	for i := range 5 {
		require.NoError(t, r.Insert(ctx, entry(fmt.Sprintf("j%d", i), t0.Add(-time.Duration(5-i)*time.Hour))))
	}
	assert.Equal(t, []string{"j4", "j3", "j2"}, ids(t, r), "the oldest started_at go first")

	assert.Equal(t, 10000, jobhistory.MaxRows)
	assert.Equal(t, 30*24*time.Hour, jobhistory.Retention)
}

func TestSchema_HistoryListingAndPruningUseTheIndex(t *testing.T) {
	db := testdb.Open(t)
	for _, q := range []string{
		`SELECT id FROM mcp_job_history ORDER BY started_at DESC, id DESC LIMIT 20`,
		`SELECT id FROM mcp_job_history WHERE state IN ('queued', 'running') ORDER BY started_at DESC, id DESC LIMIT 20`,
	} {
		rows, err := db.Reader().Query(`EXPLAIN QUERY PLAN ` + q)
		require.NoError(t, err)
		var plan string
		for rows.Next() {
			var id, parent, notUsed int
			var detail string
			require.NoError(t, rows.Scan(&id, &parent, &notUsed, &detail))
			plan += detail + "\n"
		}
		require.NoError(t, rows.Err())
		_ = rows.Close()
		assert.Contains(t, plan, "idx_mcp_job_history", q)
		assert.NotContains(t, plan, "SCAN mcp_job_history\n", q)
	}
}
