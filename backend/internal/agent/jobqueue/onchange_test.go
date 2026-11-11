package jobqueue_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestQueue_PanickingOnChangeCallbackDoesNotStopTheWorker(t *testing.T) {
	_, repo := newRepo(t)
	q := jobqueue.New(repo, jobqueue.Options{Workers: 1})
	q.Register(domain.JobGenerate, func(context.Context, domain.Job) error { return nil }, false)
	q.OnChange(func(string) { panic("callback boom") }) // an unrecovered panic here would end the test binary
	q.Start(context.Background())
	defer q.Stop()

	first, err := q.Enqueue(context.Background(), domain.JobGenerate, struct{}{})
	require.NoError(t, err)
	second, err := q.Enqueue(context.Background(), domain.JobGenerate, struct{}{})
	require.NoError(t, err)

	waitStatus(t, repo, first.ID, domain.JobSucceeded)
	waitStatus(t, repo, second.ID, domain.JobSucceeded)
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

	// Enqueue's own notification may append after the worker's last one: wait for all three (queued, running, final); one must have read the final status.
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(seen) >= 3 }, 5*time.Second, 5*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, seen, domain.JobSucceeded, "the notification after the final status already sees it")
}
