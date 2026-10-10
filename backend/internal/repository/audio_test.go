package repository_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestEditorRepository_EveryBranchNumbersItsOwnAudioTimelineVersions(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	seedBranch(t, f, "b1", "別展開")
	repo := repository.NewEditorRepository(f.db)
	tl := func(mix string) domain.AudioTimeline {
		return domain.AudioTimeline{Mode: domain.AudioModeDirector, MixPath: mix, TargetLUFS: -14, Clips: []domain.AudioClip{}}
	}

	t1, err := repo.SaveAudioTimeline(ctx, f.projectID, "", tl("trunk-1.m4a"))
	require.NoError(t, err)
	b1, err := repo.SaveAudioTimeline(ctx, f.projectID, "b1", tl("branch-1.m4a"))
	require.NoError(t, err)
	t2, err := repo.SaveAudioTimeline(ctx, f.projectID, "", tl("trunk-2.m4a"))
	require.NoError(t, err)

	assert.Equal(t, []int{1, 1, 2}, []int{t1.VersionNumber, b1.VersionNumber, t2.VersionNumber})
	latestTrunk, err := repo.LatestAudioTimeline(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, t2.ID, latestTrunk.ID)
	latestBranch, err := repo.LatestAudioTimeline(ctx, f.projectID, "b1")
	require.NoError(t, err)
	assert.Equal(t, b1.ID, latestBranch.ID)
	assert.Equal(t, "b1", latestBranch.BranchID)
	assert.Equal(t, domain.AudioModeDirector, latestBranch.Timeline.Mode)

	trunkVersions, err := repo.ListAudioTimelines(ctx, f.projectID, "")
	require.NoError(t, err)
	require.Len(t, trunkVersions, 2)
	assert.Equal(t, []int{2, 1}, []int{trunkVersions[0].VersionNumber, trunkVersions[1].VersionNumber}, "newest first")
	branchVersions, err := repo.ListAudioTimelines(ctx, f.projectID, "b1")
	require.NoError(t, err)
	assert.Len(t, branchVersions, 1)
}

func TestEditorRepository_AudioTimelineOfAForeignBranchIsNotFound(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	seedProject(t, f.db, "other")
	repo := repository.NewEditorRepository(f.db)

	_, err := repo.SaveAudioTimeline(ctx, "other", "no-such-branch", domain.AudioTimeline{})

	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = repo.LatestAudioTimeline(ctx, "other", "no-such-branch")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestVoiceRepository_ASpeakerHasOneVoiceAndAClonedOneReplacesTheOld(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedProject(t, db, "p1")
	seedProject(t, db, "p2")
	repo := repository.NewVoiceRepository(db)

	_, err := repo.Save(ctx, "p1", "美咲", "美咲（初版）", "voice-1")
	require.NoError(t, err)
	second, err := repo.Save(ctx, "p1", "美咲", "美咲", "voice-2")
	require.NoError(t, err)
	_, err = repo.Save(ctx, "p1", "健", "健", "voice-3")
	require.NoError(t, err)
	_, err = repo.Save(ctx, "p2", "美咲", "他の Project", "voice-9")
	require.NoError(t, err)

	got, err := repo.List(ctx, "p1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	bySpeaker := map[string]domain.VoiceClone{}
	for _, v := range got {
		bySpeaker[v.Speaker] = v
	}
	assert.Equal(t, "voice-2", bySpeaker["美咲"].VoiceID)
	assert.Equal(t, second.ID, bySpeaker["美咲"].ID)
	assert.Equal(t, "voice-3", bySpeaker["健"].VoiceID)
	_, err = repo.Save(ctx, "nope", "x", "x", "v")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestCostRepository_ProjectBudgetSumsTheLedgerAndAddCostAppendsToIt(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedProject(t, db, "p1")
	require.NoError(t, exec(t, db, `UPDATE project SET project_budget_ceiling = 5 WHERE id = 'p1'`))
	repo := repository.NewCostRepository(db)

	require.NoError(t, repo.AddCost(ctx, domain.CostEntry{ProjectID: "p1", Provider: domain.ModelElevenLabsTTS, EstimatedCost: 0.25}))
	require.NoError(t, repo.AddCost(ctx, domain.CostEntry{ProjectID: "p1", Provider: domain.ModelSyncLabs, EstimatedCost: 1.5}))
	ceiling, spent, err := repo.ProjectBudget(ctx, "p1")

	require.NoError(t, err)
	require.NotNil(t, ceiling)
	assert.InDelta(t, 5, *ceiling, 1e-9)
	assert.InDelta(t, 1.75, spent, 1e-9)
	var providers []string
	rows, err := db.Reader().QueryContext(ctx, `SELECT provider FROM cost_ledger_entry WHERE project_id = 'p1' ORDER BY provider`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p string
		require.NoError(t, rows.Scan(&p))
		providers = append(providers, p)
	}
	assert.Equal(t, []string{"elevenlabs-tts", "sync-labs"}, providers)

	seedProject(t, db, "p2")
	ceiling, spent, err = repo.ProjectBudget(ctx, "p2")
	require.NoError(t, err)
	assert.Nil(t, ceiling)
	assert.Zero(t, spent)
	assert.ErrorIs(t, repo.AddCost(ctx, domain.CostEntry{ProjectID: "nope", Provider: domain.ModelElevenLabsTTS}), repository.ErrNotFound)
	_, _, err = repo.ProjectBudget(ctx, "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestCostRepository_ReserveHoldsTheEstimateUnderTheProjectCeilingUntilReleased(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedProject(t, db, "p1")
	require.NoError(t, exec(t, db, `UPDATE project SET project_budget_ceiling = 1 WHERE id = 'p1'`))
	repo := repository.NewCostRepository(db)

	// Of several runs that reserve at the same time, only as many as the ceiling allows get through.
	const runs = 8
	var (
		mu       sync.Mutex
		releases []func()
		wg       sync.WaitGroup
	)
	for range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, ok, err := repo.Reserve(ctx, "p1", 0.3)
			assert.NoError(t, err)
			if ok {
				mu.Lock()
				releases = append(releases, release)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	assert.Len(t, releases, 3, "0.3 fits three times under 1")
	_, spent, err := repo.ProjectBudget(ctx, "p1")
	require.NoError(t, err)
	assert.InDelta(t, 0.9, spent, 1e-9, "the budget counts what is reserved")

	// The cost reaches the ledger, then the reservation goes: counted once.
	require.NoError(t, repo.AddCost(ctx, domain.CostEntry{ProjectID: "p1", Provider: domain.ModelElevenLabsTTS, EstimatedCost: 0.3}))
	releases[0]()
	releases[0]() // safe to call twice
	_, spent, err = repo.ProjectBudget(ctx, "p1")
	require.NoError(t, err)
	assert.InDelta(t, 0.9, spent, 1e-9)

	releases[1]()
	releases[2]()
	_, ok, err := repo.Reserve(ctx, "p1", 0.8)
	require.NoError(t, err)
	assert.False(t, ok, "0.3 is spent: 0.8 does not fit")
	release, ok, err := repo.Reserve(ctx, "p1", 0.7)
	require.NoError(t, err)
	assert.True(t, ok)
	release()

	_, _, err = repo.Reserve(ctx, "nope", 0.1)
	assert.ErrorIs(t, err, repository.ErrNotFound)
}
