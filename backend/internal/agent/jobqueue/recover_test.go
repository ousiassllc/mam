package jobqueue_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// forceRunning simulates a crash: the process died while the job was running.
func forceRunning(t *testing.T, db *repository.DB, repo *repository.JobRepository, kind domain.JobKind, attempts int) domain.Job {
	t.Helper()
	job, err := repo.Create(context.Background(), kind, nil)
	require.NoError(t, err)
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE job SET status = 'running', attempts = ?, started_at = '2026-01-01T00:00:00.000Z' WHERE id = ?`, attempts, job.ID)
		return err
	}))
	return job
}

func TestQueue_RecoverResumesResumableAndFailsTheRest(t *testing.T) {
	db, repo := newRepo(t)
	q := jobqueue.New(repo, jobqueue.Options{MaxAttempts: 3})
	var ran sync.Map
	record := func(_ context.Context, job domain.Job) error { ran.Store(job.ID, true); return nil }
	q.Register(domain.JobJudge, record, true)     // idempotent: resumable
	q.Register(domain.JobGenerate, record, false) // paid API call: never repeated silently
	q.Register(domain.JobRepair, record, true)

	resumable := forceRunning(t, db, repo, domain.JobJudge, 1)
	unsafe := forceRunning(t, db, repo, domain.JobGenerate, 1)
	exhausted := forceRunning(t, db, repo, domain.JobRepair, 3)
	queuedOnly, err := repo.Create(context.Background(), domain.JobJudge, nil)
	require.NoError(t, err)

	resumed, failed, err := q.Recover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, resumed)
	assert.Equal(t, 2, failed)

	got, err := repo.Get(context.Background(), unsafe.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobFailed, got.Status)
	assert.Equal(t, jobqueue.InterruptedReason, got.Error)
	got, err = repo.Get(context.Background(), exhausted.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobFailed, got.Status)

	q.Start(context.Background())
	defer q.Stop()
	waitStatus(t, repo, resumable.ID, domain.JobSucceeded)
	waitStatus(t, repo, queuedOnly.ID, domain.JobSucceeded)
	_, wasRun := ran.Load(unsafe.ID)
	assert.False(t, wasRun, "a failed interrupted job must not be executed")
}

func TestQueue_RecoverFailsJobsWithoutHandler(t *testing.T) {
	db, repo := newRepo(t)
	q := jobqueue.New(repo, jobqueue.Options{})
	job := forceRunning(t, db, repo, domain.JobGenerate, 1)

	_, failed, err := q.Recover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, failed)
	waitStatus(t, repo, job.ID, domain.JobFailed)
}

// A queued job of a kind nobody handles would be failed by the first worker that claims it; Recover
// fails it already, so that whoever waits for its work (a Shot) can be released before Start (#436).
func TestQueue_RecoverFailsQueuedJobsWithoutHandlerAndKeepsTheOthers(t *testing.T) {
	_, repo := newRepo(t)
	q := jobqueue.New(repo, jobqueue.Options{})
	q.Register(domain.JobGenerate, func(context.Context, domain.Job) error { return nil }, false)
	orphan, err := repo.Create(context.Background(), domain.JobJudge, nil)
	require.NoError(t, err)
	handled, err := repo.Create(context.Background(), domain.JobGenerate, nil)
	require.NoError(t, err)

	_, failed, err := q.Recover(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, failed)
	got, err := repo.Get(context.Background(), orphan.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobFailed, got.Status)
	got, err = repo.Get(context.Background(), handled.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobQueued, got.Status)
}

func TestQueue_StopRequeuesInterruptedJobSoNextStartResumesIt(t *testing.T) {
	_, repo := newRepo(t)
	started := make(chan struct{})
	q := jobqueue.New(repo, jobqueue.Options{})
	q.Register(domain.JobGenerate, func(ctx context.Context, _ domain.Job) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, true)
	q.Start(context.Background())
	job, err := q.Enqueue(context.Background(), domain.JobGenerate, nil)
	require.NoError(t, err)
	<-started
	q.Stop()

	got, err := repo.Get(context.Background(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobQueued, got.Status, "graceful shutdown must not leave the job running")

	// A second process picks it up.
	q2 := jobqueue.New(repo, jobqueue.Options{})
	q2.Register(domain.JobGenerate, func(context.Context, domain.Job) error { return nil }, true)
	q2.Start(context.Background())
	defer q2.Stop()
	done := waitStatus(t, repo, job.ID, domain.JobSucceeded)
	assert.Equal(t, 2, done.Attempts)
}

func TestQueue_StopFailsInterruptedNonResumableJobInsteadOfRequeueing(t *testing.T) {
	_, repo := newRepo(t)
	started := make(chan struct{})
	q := jobqueue.New(repo, jobqueue.Options{})
	q.Register(domain.JobGenerate, func(ctx context.Context, _ domain.Job) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, false)
	q.Start(context.Background())
	job, err := q.Enqueue(context.Background(), domain.JobGenerate, nil)
	require.NoError(t, err)
	<-started
	q.Stop()

	got, err := repo.Get(context.Background(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobFailed, got.Status, "a billed, non-resumable job must not run again on the next start")
	assert.Equal(t, jobqueue.ShutdownReason, got.Error)
}
