package jobqueue_test

import (
	"context"
	"testing"

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
