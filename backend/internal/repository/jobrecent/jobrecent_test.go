package jobrecent_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobrecent"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

func TestListRecent_KeepsEveryUnfinishedJobAndOnlyTheNewestOthers(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	jobs := repository.NewJobRepository(db)
	running, err := jobs.Create(ctx, domain.JobGenerate, []byte(`{"shot_id":"s1"}`))
	require.NoError(t, err)
	require.NoError(t, jobs.MarkRunning(ctx, running.ID))
	var newest domain.Job
	for range 3 {
		newest, err = jobs.Create(ctx, domain.JobGentest, nil)
		require.NoError(t, err)
		require.NoError(t, jobs.MarkRunning(ctx, newest.ID))
		require.NoError(t, jobs.MarkSucceeded(ctx, newest.ID))
	}

	got, err := jobrecent.New(db).ListRecent(ctx, 1)

	require.NoError(t, err)
	require.Len(t, got, 2, "the running job and the newest finished one")
	assert.Equal(t, newest.ID, got[0].ID, "newest first")
	assert.Equal(t, domain.JobSucceeded, got[0].Status)
	assert.NotNil(t, got[0].StartedAt)
	assert.NotNil(t, got[0].FinishedAt)
	assert.Equal(t, running.ID, got[1].ID, "a job that has not finished is listed however old it is")
	assert.JSONEq(t, `{"shot_id":"s1"}`, string(got[1].Payload))
	assert.Nil(t, got[1].FinishedAt)
}

func TestListRecent_OfAnEmptyTableIsEmpty(t *testing.T) {
	got, err := jobrecent.New(testdb.Open(t)).ListRecent(t.Context(), 50)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// The list is in the order the jobs started, not the order they were queued: a job queued first that started last is on top, and the newest
// N are the newest by that order too. A job that has not started is where it was queued.
func TestListRecent_IsInTheOrderOfTheStartNotOfTheQueue(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	insert := func(id, status, created string, started any) {
		require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO job (id, kind, payload, status, created_at, started_at) VALUES (?, 'gentest', '{}', ?, ?, ?)`, id, status, created, started)
			return err
		}))
	}
	insert("a-queued-first-started-last", "succeeded", "2026-11-03T10:00:00.000Z", "2026-11-03T10:30:00.000Z")
	insert("b-queued-second-started-first", "succeeded", "2026-11-03T10:01:00.000Z", "2026-11-03T10:05:00.000Z")
	insert("c-waiting", "queued", "2026-11-03T10:10:00.000Z", nil)

	got, err := jobrecent.New(db).ListRecent(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 2, "the waiting one and the newest started")
	assert.Equal(t, "a-queued-first-started-last", got[0].ID, "started at 10:30")
	assert.Equal(t, "c-waiting", got[1].ID, "queued at 10:10")

	got, err = jobrecent.New(db).ListRecent(ctx, 50)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"a-queued-first-started-last", "c-waiting", "b-queued-second-started-first"}, []string{got[0].ID, got[1].ID, got[2].ID})
}

// The list reads the failure a job kept as the common error (#1010), so that the job list screen can draw the error box for it.
func TestListRecent_ReadsTheCommonErrorOfAFailedJob(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	jobs := repository.NewJobRepository(db)
	failed, err := jobs.Create(ctx, domain.JobGenerate, nil)
	require.NoError(t, err)
	require.NoError(t, jobs.MarkFailedWithInfo(ctx, failed.ID, "ディスクの空きが足りません", `{"code":"disk_full","detail":"no space left on device"}`))

	got, err := jobrecent.New(db).ListRecent(ctx, 10)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "ディスクの空きが足りません", got[0].Error)
	assert.JSONEq(t, `{"code":"disk_full","detail":"no space left on device"}`, got[0].ErrorInfo)
}
