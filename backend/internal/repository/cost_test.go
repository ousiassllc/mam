package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// newTake stores a Take with a video file of size bytes and returns its ID.
func newTake(t *testing.T, f genRepoFixture, shotID, file string, size int64, est float64, actual *float64) string {
	t.Helper()
	pv, err := f.repo.SavePromptedSpec(context.Background(), shotID, domain.ShotSpec{ShotObjective: "x", Camera: domain.SpecCamera{Framing: "wide"}}, nil)
	require.NoError(t, err)
	id, err := f.repo.CreateTake(context.Background(), repository.NewTake{
		ProjectID: f.projectID, ShotID: shotID, PromptVersionID: pv.ID, Model: domain.ModelKling3,
		Asset:         domain.Asset{ID: "a-" + file, ProjectID: f.projectID, Type: domain.AssetVideo, FilePath: file, MimeType: "video/mp4", SizeBytes: size},
		EstimatedCost: est, ActualCost: actual,
	})
	require.NoError(t, err)
	return id
}

func TestCostRepository_SummarisesEstimatedAndActualByShotSceneAndProject(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	shots, err := f.repo.ListProjectShots(ctx, f.projectID)
	require.NoError(t, err)
	shotA, shotB := shots[0].Shot.ID, shots[1].Shot.ID

	newTake(t, f, shotA, "a1.mp4", 10, 1.5, ptr(1.25))
	newTake(t, f, shotA, "a2.mp4", 10, 0.5, nil) // the vendor reported nothing for this one
	newTake(t, f, shotB, "b1.mp4", 10, 2, ptr(2.5))
	require.NoError(t, f.repo.SetShotState(ctx, shotB, domain.ShotPlanning))
	require.NoError(t, repository.NewPolicyRepository(f.db).SetBudget(ctx, f.projectID, ptr(20.0), nil))

	sum, err := repository.NewCostRepository(f.db).Summary(ctx, f.projectID)
	require.NoError(t, err)

	assert.InDelta(t, 4, sum.Project.Estimated, 1e-9)
	assert.InDelta(t, 3.75, sum.Project.Actual, 1e-9)
	assert.Equal(t, 3, sum.Project.Entries)
	assert.Equal(t, 2, sum.Project.ActualEntries)
	assert.False(t, sum.Project.ActualKnown(), "one entry has no reported cost")
	assert.InDelta(t, 20, *sum.ProjectCeiling, 1e-9)
	assert.Nil(t, sum.ShotCeiling)

	require.Len(t, sum.Scenes, 1)
	scene := sum.Scenes[0]
	assert.Equal(t, "A", scene.Title)
	assert.InDelta(t, 4, scene.Totals.Estimated, 1e-9)
	require.Len(t, scene.Shots, 2)
	assert.InDelta(t, 2, scene.Shots[0].Totals.Estimated, 1e-9, "Shot 1: 1.5 + 0.5")
	assert.False(t, scene.Shots[0].Totals.ActualKnown())
	assert.InDelta(t, 2, scene.Shots[1].Totals.Estimated, 1e-9)
	assert.True(t, scene.Shots[1].Totals.ActualKnown())
	assert.InDelta(t, 2.5, scene.Shots[1].Totals.Actual, 1e-9)
}

func TestCostRepository_ListsShotsWithoutCostAndKeepsEntriesOfDeletedShots(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	repo := repository.NewCostRepository(f.db)

	sum, err := repo.Summary(ctx, f.projectID)
	require.NoError(t, err)
	require.Len(t, sum.Scenes, 1)
	assert.Len(t, sum.Scenes[0].Shots, 2, "every Shot is listed, also when nothing was spent on it")
	assert.Zero(t, sum.Project.Estimated)

	newTake(t, f, f.shotID, "a1.mp4", 10, 3, nil)
	err = f.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM shot WHERE id = ?`, f.shotID)
		return err
	})
	require.NoError(t, err)

	sum, err = repo.Summary(ctx, f.projectID)
	require.NoError(t, err)
	assert.InDelta(t, 3, sum.Unassigned.Estimated, 1e-9, "the ledger entry outlives its Shot (ON DELETE SET NULL)")
	assert.InDelta(t, 3, sum.Project.Estimated, 1e-9, "and still counts for the Project")

	_, err = repo.Summary(ctx, "missing")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}
