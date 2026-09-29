package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestProjectRepository_TransitionFollowsTheCoreStateMachine(t *testing.T) {
	ctx := context.Background()
	repo, _, _ := newProjectFixture(t)
	id := createProject(t, repo, domain.StateDraft)

	require.NoError(t, repo.Transition(ctx, id, domain.StatePlanning))
	require.NoError(t, repo.Transition(ctx, id, domain.StatePlanning), "moving to the current state is a no-op")

	err := repo.Transition(ctx, id, domain.StateGenerating)
	assert.ErrorIs(t, err, repository.ErrInvalidTransition, "PLANNING cannot skip VISUAL_DESIGN")
	p, err := repo.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.StatePlanning, p.State, "a rejected move changes nothing")

	require.NoError(t, repo.Transition(ctx, id, domain.StateAwaitingApproval))
	require.NoError(t, repo.Transition(ctx, id, domain.StateVisualDesign))

	assert.ErrorIs(t, repo.Transition(ctx, "missing", domain.StatePlanning), repository.ErrNotFound)
}

func TestPlanRepository_SaveScriptAndSavePlan(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	projects := repository.NewProjectRepository(db)
	plans := repository.NewPlanRepository(db)
	id := createProject(t, projects, domain.StatePlanning)

	_, err := plans.LatestScript(ctx, id)
	assert.ErrorIs(t, err, repository.ErrNotFound)

	v1, err := plans.SaveScript(ctx, id, domain.ScriptDoc{Script: domain.Script{Title: "一"}})
	require.NoError(t, err)
	v2, err := plans.SaveScript(ctx, id, domain.ScriptDoc{Script: domain.Script{Title: "二"}})
	require.NoError(t, err)
	assert.Equal(t, 1, v1.VersionNumber)
	assert.Equal(t, 2, v2.VersionNumber)
	latest, err := plans.LatestScript(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, v2.ID, latest.ID)
	assert.Equal(t, "二", latest.Doc.Script.Title)
	_, err = plans.SaveScript(ctx, "missing", domain.ScriptDoc{})
	assert.ErrorIs(t, err, repository.ErrNotFound)

	plan := domain.ScenePlan{Scenes: []domain.PlannedScene{
		{Title: "朝", Location: "寝室", Shots: []domain.PlannedShot{{Objective: "光", DurationSeconds: 3}, {Objective: "目覚め", DurationSeconds: 2}}},
		{Title: "夜", Location: "寝室", Shots: []domain.PlannedShot{{Objective: "眠る", DurationSeconds: 4}}},
	}}
	board := domain.Storyboard{Shots: [][]domain.ShotStoryboard{{{Description: "窓"}, {Description: "目"}}, {{Description: "枕"}}}}
	require.NoError(t, plans.SavePlan(ctx, id, plan, board))

	scenes, err := plans.ListScenes(ctx, id)
	require.NoError(t, err)
	require.Len(t, scenes, 2)
	assert.Equal(t, "夜", scenes[1].Title)
	assert.Equal(t, scenes[0].Location, scenes[1].Location, "a Location named twice is one row")
	counts, err := plans.ShotCounts(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 2, counts[scenes[0].ID])
	shots, err := plans.ListShots(ctx, scenes[0].ID)
	require.NoError(t, err)
	require.Len(t, shots, 2)
	assert.Equal(t, 3.0, shots[0].Duration)

	assert.ErrorIs(t, plans.SavePlan(ctx, id, plan, board), repository.ErrConflict, "a plan never overwrites existing Scenes")
	scenes, err = plans.ListScenes(ctx, id)
	require.NoError(t, err)
	assert.Len(t, scenes, 2)
}
