package jobqueue_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A job that failed keeps the failure as the common error (#1010) next to its reason: the kind of failure and what the machine said (with the
// secrets hidden); a failure nobody knows has a correlation id, which the log line of the failure carries.
func TestQueue_AFailedJobKeepsTheCommonError(t *testing.T) {
	_, repo := newRepo(t)
	q := jobqueue.New(repo, jobqueue.Options{Workers: 1})
	q.Register(domain.JobJudge, func(context.Context, domain.Job) error {
		return &modeladapter.ProviderError{Vendor: "endpoint:bundled-comfyui", Status: 422, Message: "KSampler: CUDA out of memory"}
	}, false)
	q.Register(domain.JobRepair, func(context.Context, domain.Job) error {
		return errors.New("odd failure with Authorization: Bearer abcdefghijklmnop0000")
	}, false)
	q.Register(domain.JobGenerate, func(context.Context, domain.Job) error { return nil }, false)
	q.Start(context.Background())
	defer q.Stop()

	known, err := q.Enqueue(context.Background(), domain.JobJudge, nil)
	require.NoError(t, err)
	unknown, err := q.Enqueue(context.Background(), domain.JobRepair, nil)
	require.NoError(t, err)
	fine, err := q.Enqueue(context.Background(), domain.JobGenerate, nil)
	require.NoError(t, err)

	e, ok := apperr.Unmarshal(waitStatus(t, repo, known.ID, domain.JobFailed).ErrorInfo)
	require.True(t, ok)
	assert.Equal(t, apperr.CodeVRAM, e.Code)
	assert.Contains(t, e.Detail, "CUDA out of memory")
	assert.Empty(t, e.ID, "a known failure has no correlation id")

	odd, ok := apperr.Unmarshal(waitStatus(t, repo, unknown.ID, domain.JobFailed).ErrorInfo)
	require.True(t, ok)
	assert.Equal(t, apperr.CodeUnknown, odd.Code)
	assert.NotEmpty(t, odd.ID)
	assert.NotContains(t, odd.Detail, "abcdefghijklmnop0000", "a secret is hidden in the detail")

	assert.Empty(t, waitStatus(t, repo, fine.ID, domain.JobSucceeded).ErrorInfo)
}
