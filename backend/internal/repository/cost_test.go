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
	pv, err := f.repo.SavePromptedSpec(context.Background(), shotID, domain.ShotSpec{ShotObjective: "x", Camera: domain.SpecCamera{Framing: "wide"}}, nil, nil)
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

func TestCostRepository_KeepsTrunkAndBranchScenesApartWhenTheyShareAnOrderIndex(t *testing.T) {
	ctx := context.Background()
	f := newBranchFixture(t)
	trunkScenes, err := f.plans.ListScenes(ctx, f.projectID)
	require.NoError(t, err)
	require.Len(t, trunkScenes, 1)
	trunkShots, err := repository.NewGenerationRepository(f.db).ListProjectShots(ctx, f.projectID)
	require.NoError(t, err)
	require.Len(t, trunkShots, 2)

	b, err := f.branches.Create(ctx, repository.NewBranch{ShotID: f.shotID, PointType: domain.BranchAtShot, Name: "Alt"})
	require.NoError(t, err)
	scene, err := f.plans.CreateScene(ctx, domain.Scene{ProjectID: f.projectID, BranchID: b.ID, Title: "Door", Location: "Hall"})
	require.NoError(t, err)
	require.Equal(t, trunkScenes[0].OrderIndex, scene.OrderIndex, "the Branch Scene shares the trunk Scene's order_index")
	var branchShots []string
	for _, objective := range []string{"open", "close"} {
		sh, err := f.plans.CreateShot(ctx, domain.Shot{SceneID: scene.ID, Objective: objective, Duration: 2})
		require.NoError(t, err)
		branchShots = append(branchShots, sh.ID)
	}

	gen := f.genRepoFixture
	newTake(t, gen, trunkShots[0].Shot.ID, "t0.mp4", 10, 1, nil)
	newTake(t, gen, trunkShots[1].Shot.ID, "t1.mp4", 10, 2, nil)
	newTake(t, gen, branchShots[0], "b0.mp4", 10, 4, nil)
	newTake(t, gen, branchShots[1], "b1.mp4", 10, 8, nil)

	sum, err := repository.NewCostRepository(f.db).Summary(ctx, f.projectID)
	require.NoError(t, err)
	require.Len(t, sum.Scenes, 2, "one entry per Scene, not one per run of rows")
	trunk, branch := sum.Scenes[0], sum.Scenes[1]
	assert.Empty(t, trunk.BranchID)
	assert.Equal(t, b.ID, branch.BranchID)
	assert.Equal(t, "Alt", branch.BranchName)
	require.Len(t, trunk.Shots, 2)
	require.Len(t, branch.Shots, 2)
	assert.Equal(t, trunkShots[0].Shot.ID, trunk.Shots[0].ShotID)
	assert.Equal(t, trunkShots[1].Shot.ID, trunk.Shots[1].ShotID)
	assert.Equal(t, branchShots, []string{branch.Shots[0].ShotID, branch.Shots[1].ShotID})
	assert.InDelta(t, 3, trunk.Totals.Estimated, 1e-9)
	assert.InDelta(t, 12, branch.Totals.Estimated, 1e-9)
	assert.InDelta(t, 15, sum.Project.Estimated, 1e-9)
}
