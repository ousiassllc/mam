package jobqueue_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func newRepo(t *testing.T) (*repository.DB, *repository.JobRepository) {
	t.Helper()
	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, repository.NewJobRepository(db)
}

func waitStatus(t *testing.T, repo *repository.JobRepository, id string, want domain.JobStatus) domain.Job {
	t.Helper()
	var job domain.Job
	require.Eventually(t, func() bool {
		var err error
		job, err = repo.Get(context.Background(), id)
		return err == nil && job.Status == want
	}, 5*time.Second, 5*time.Millisecond, "job %s never reached %s (last: %s)", id, want, job.Status)
	return job
}

func TestQueue_RunsJobsAsynchronouslyWithPayload(t *testing.T) {
	_, repo := newRepo(t)
	q := jobqueue.New(repo, jobqueue.Options{})
	got := make(chan string, 1)
	q.Register(domain.JobGenerate, func(_ context.Context, job domain.Job) error {
		got <- string(job.Payload)
		return nil
	}, false)
	q.Start(context.Background())
	defer q.Stop()

	job, err := q.Enqueue(context.Background(), domain.JobGenerate, map[string]string{"shot_id": "s1"})
	require.NoError(t, err)

	assert.JSONEq(t, `{"shot_id":"s1"}`, <-got)
	done := waitStatus(t, repo, job.ID, domain.JobSucceeded)
	assert.Equal(t, 1, done.Attempts)
}

func TestQueue_FailedHandlerAndPanicAreRecordedAndDoNotStopWorkers(t *testing.T) {
	_, repo := newRepo(t)
	q := jobqueue.New(repo, jobqueue.Options{Workers: 1})
	q.Register(domain.JobJudge, func(context.Context, domain.Job) error { return errors.New("judge exploded") }, false)
	q.Register(domain.JobRepair, func(context.Context, domain.Job) error { panic("nil map") }, false)
	q.Register(domain.JobGenerate, func(context.Context, domain.Job) error { return nil }, false)
	q.Start(context.Background())
	defer q.Stop()

	failing, err := q.Enqueue(context.Background(), domain.JobJudge, nil)
	require.NoError(t, err)
	panicking, err := q.Enqueue(context.Background(), domain.JobRepair, nil)
	require.NoError(t, err)
	ok, err := q.Enqueue(context.Background(), domain.JobGenerate, nil)
	require.NoError(t, err)

	assert.Equal(t, "judge exploded", waitStatus(t, repo, failing.ID, domain.JobFailed).Error)
	assert.Contains(t, waitStatus(t, repo, panicking.ID, domain.JobFailed).Error, "nil map")
	waitStatus(t, repo, ok.ID, domain.JobSucceeded)
}

func TestQueue_EnqueueWithoutHandlerFails(t *testing.T) {
	_, repo := newRepo(t)
	q := jobqueue.New(repo, jobqueue.Options{})
	_, err := q.Enqueue(context.Background(), domain.JobGenerate, nil)
	assert.Error(t, err)
	queued, err := repo.ListByStatus(context.Background(), domain.JobQueued)
	require.NoError(t, err)
	assert.Empty(t, queued, "nothing may be persisted for an unroutable job")
}

func TestQueue_RespectsWorkerLimit(t *testing.T) {
	_, repo := newRepo(t)
	const workers, total = 2, 6
	var cur, peak atomic.Int32
	q := jobqueue.New(repo, jobqueue.Options{Workers: workers})
	q.Register(domain.JobGenerate, func(context.Context, domain.Job) error {
		n := cur.Add(1)
		defer cur.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		return nil
	}, false)
	q.Start(context.Background())
	defer q.Stop()

	ids := make([]string, total)
	for i := range ids {
		job, err := q.Enqueue(context.Background(), domain.JobGenerate, nil)
		require.NoError(t, err)
		ids[i] = job.ID
	}
	for _, id := range ids {
		waitStatus(t, repo, id, domain.JobSucceeded)
	}
	assert.EqualValues(t, workers, peak.Load(), "jobs run concurrently but never beyond the worker limit")
}

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

func TestQueue_OnChangeNotifiesAfterTheFinalStatusIsPersisted(t *testing.T) {
	_, repo := newRepo(t)
	q := jobqueue.New(repo, jobqueue.Options{})
	q.Register(domain.JobGenerate, func(context.Context, domain.Job) error { return nil }, false)

	var mu sync.Mutex
	var seen []domain.JobStatus
	q.OnChange(func(id string) {
		job, err := repo.Get(context.Background(), id)
		require.NoError(t, err)
		mu.Lock()
		seen = append(seen, job.Status)
		mu.Unlock()
	})
	q.Start(context.Background())
	defer q.Stop()

	job, err := q.Enqueue(context.Background(), domain.JobGenerate, struct{}{})
	require.NoError(t, err)
	waitStatus(t, repo, job.ID, domain.JobSucceeded)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) > 0 && seen[len(seen)-1] == domain.JobSucceeded
	}, 5*time.Second, 5*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, domain.JobSucceeded, seen[len(seen)-1], "the last notification already sees the final status")
}

func TestQueue_TransientClaimErrorDoesNotStrandQueuedJobs(t *testing.T) {
	_, repo := newRepo(t)
	q := jobqueue.New(repo, jobqueue.Options{Workers: 1})
	ran := make(chan struct{}, 1)
	q.Register(domain.JobGenerate, func(context.Context, domain.Job) error {
		ran <- struct{}{}
		return nil
	}, false)

	// Persist the job before the workers start so that no later Enqueue/signal can rescue the worker.
	job, err := repo.Create(context.Background(), domain.JobGenerate, []byte(`{}`))
	require.NoError(t, err)

	realClaim := q.ClaimFunc()
	var calls atomic.Int32
	q.SetClaim(func(ctx context.Context) (domain.Job, error) {
		switch calls.Add(1) {
		case 1:
			// Uses up the wake-up Start sends, so only a retry can reach the job afterwards.
			return domain.Job{}, repository.ErrNotFound
		case 2:
			return domain.Job{}, errors.New("database is locked")
		default:
			return realClaim(ctx)
		}
	})
	q.SetClaimBackoff(5 * time.Millisecond)
	q.Start(context.Background())
	defer q.Stop()

	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("queued job was not run after a transient ClaimNext error")
	}
	waitStatus(t, repo, job.ID, domain.JobSucceeded)
}
