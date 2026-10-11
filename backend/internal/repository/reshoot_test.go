package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestEditorRepository_EachBranchKeepsItsOwnTimelineVersions(t *testing.T) {
	ctx := context.Background()
	f := newBranchFixture(t)
	repo := repository.NewEditorRepository(f.db)
	b, err := f.branches.Create(ctx, repository.NewBranch{ShotID: f.shotID, PointType: domain.BranchAtShot, Name: "A"})
	require.NoError(t, err)

	trunk1, err := repo.SaveEditTimeline(ctx, f.projectID, "", "", timelineOf(4))
	require.NoError(t, err)
	branch1, err := repo.SaveEditTimeline(ctx, f.projectID, b.ID, "", timelineOf(2))
	require.NoError(t, err)
	branch2, err := repo.SaveEditTimeline(ctx, f.projectID, b.ID, branch1.ID, timelineOf(3))
	require.NoError(t, err)

	assert.Equal(t, 1, trunk1.VersionNumber)
	assert.Equal(t, 1, branch1.VersionNumber, "the Branch numbers its own versions")
	assert.Equal(t, 2, branch2.VersionNumber)
	assert.Equal(t, b.ID, branch2.BranchID)

	latestTrunk, err := repo.LatestEditTimeline(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, trunk1.ID, latestTrunk.ID, "an edit of the Branch never becomes the trunk's latest")
	latestBranch, err := repo.LatestEditTimeline(ctx, f.projectID, b.ID)
	require.NoError(t, err)
	assert.Equal(t, branch2.ID, latestBranch.ID)
	assert.InDelta(t, 3, latestBranch.Timeline.Duration(), 1e-9)

	// The trunk's version is stale for the Branch and the other way round.
	_, err = repo.SaveEditTimeline(ctx, f.projectID, b.ID, trunk1.ID, timelineOf(1))
	assert.ErrorIs(t, err, repository.ErrStale)
	_, err = repo.SaveEditTimeline(ctx, f.projectID, "missing", "", timelineOf(1))
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestEditorRepository_ReshootRoundsAreCountedPerSeriesAndAppliedOnce(t *testing.T) {
	ctx := context.Background()
	f := newBranchFixture(t)
	repo := repository.NewEditorRepository(f.db)
	base, err := repo.SaveEditTimeline(ctx, f.projectID, "", "", timelineOf(4))
	require.NoError(t, err)

	rounds, err := repo.ReshootRounds(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Zero(t, rounds)

	first, err := repo.CreateReshootRound(ctx, base.ID, []repository.NewReshoot{
		{ShotID: f.shotID, AfterShotID: f.shotID, Kind: domain.ClipReaction, Objective: "驚く", DurationSeconds: 2},
		{ShotID: f.shotID, Kind: domain.ClipInsert, Objective: "商品", DurationSeconds: 1.5},
	})
	require.NoError(t, err)
	require.Len(t, first, 2)
	assert.Equal(t, []int{1, 1}, []int{first[0].Round, first[1].Round})
	second, err := repo.CreateReshootRound(ctx, base.ID, []repository.NewReshoot{{ShotID: f.shotID, Kind: domain.ClipInsert, Objective: "商品", DurationSeconds: 1}})
	require.NoError(t, err)
	assert.Equal(t, 2, second[0].Round)

	rounds, err = repo.ReshootRounds(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, 2, rounds)
	pending, err := repo.PendingReshoots(ctx, f.projectID)
	require.NoError(t, err)
	assert.Len(t, pending, 3)
	ids, err := repo.ProjectsWithPendingReshoots(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{f.projectID}, ids)

	// Applying saves the next version and marks the round; a request cannot be applied twice.
	ver, err := repo.SaveReshootApplied(ctx, f.projectID, "", base.ID, timelineOf(6), []string{first[0].ID, first[1].ID})
	require.NoError(t, err)
	assert.Equal(t, base.VersionNumber+1, ver.VersionNumber)
	pending, err = repo.PendingReshoots(ctx, f.projectID)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, second[0].ID, pending[0].ID)
	all, err := repo.ListReshoots(ctx, f.projectID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReshootApplied, all[0].Status)
	assert.Equal(t, ver.ID, all[0].AppliedEditTimelineVersionID)

	_, err = repo.SaveReshootApplied(ctx, f.projectID, "", ver.ID, timelineOf(7), []string{first[0].ID})
	assert.ErrorIs(t, err, repository.ErrConflict)
	latest, err := repo.LatestEditTimeline(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, ver.ID, latest.ID, "the refused apply saved no version")

	_, err = repo.CreateReshootRound(ctx, "missing", nil)
	assert.ErrorIs(t, err, repository.ErrNotFound)
}
