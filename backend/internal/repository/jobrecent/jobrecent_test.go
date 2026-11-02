package jobrecent_test

import (
	"context"
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
