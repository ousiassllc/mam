package repository_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestJobRepository_Lifecycle(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewJobRepository(openTestDB(t))

	job, err := repo.Create(ctx, domain.JobGenerate, json.RawMessage(`{"shot_id":"s1"}`))
	require.NoError(t, err)

	got, err := repo.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobQueued, got.Status)
	assert.JSONEq(t, `{"shot_id":"s1"}`, string(got.Payload))
	assert.Nil(t, got.StartedAt)

	require.NoError(t, repo.MarkRunning(ctx, job.ID))
	assert.ErrorIs(t, repo.MarkRunning(ctx, job.ID), repository.ErrNotFound, "a running job cannot be claimed twice")

	got, err = repo.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobRunning, got.Status)
	assert.Equal(t, 1, got.Attempts)
	assert.NotNil(t, got.StartedAt)

	require.NoError(t, repo.Requeue(ctx, job.ID))
	require.NoError(t, repo.MarkRunning(ctx, job.ID))
	require.NoError(t, repo.MarkSucceeded(ctx, job.ID))

	got, err = repo.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobSucceeded, got.Status)
	assert.Equal(t, 2, got.Attempts)
	assert.NotNil(t, got.FinishedAt)

	assert.ErrorIs(t, repo.MarkFailed(ctx, job.ID, "late"), repository.ErrNotFound, "terminal states never change")
}

func TestJobRepository_ListByStatusOldestFirstAndDefaultPayload(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewJobRepository(openTestDB(t))

	first, err := repo.Create(ctx, domain.JobJudge, nil)
	require.NoError(t, err)
	second, err := repo.Create(ctx, domain.JobRepair, nil)
	require.NoError(t, err)
	require.NoError(t, repo.MarkRunning(ctx, second.ID))

	queued, err := repo.ListByStatus(ctx, domain.JobQueued)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	assert.Equal(t, first.ID, queued[0].ID)
	assert.JSONEq(t, `{}`, string(queued[0].Payload))

	running, err := repo.ListByStatus(ctx, domain.JobRunning)
	require.NoError(t, err)
	require.Len(t, running, 1)
	assert.Equal(t, second.ID, running[0].ID)
}

func TestJobRepository_GetMissing(t *testing.T) {
	repo := repository.NewJobRepository(openTestDB(t))
	_, err := repo.Get(context.Background(), "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}
