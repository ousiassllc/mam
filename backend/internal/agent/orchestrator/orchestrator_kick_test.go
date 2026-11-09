package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestKick_StartsAPlanJobOnlyWhereThePipelineHasWork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	incomplete := domain.Brief{Subject: "美容液"}
	tests := []struct {
		name string
		seed seed
		want bool
	}{
		{"DRAFT with a complete Brief", seed{brief: completeBrief()}, true},
		{"DRAFT with a missing Brief item", seed{level: domain.ConfirmKeyCheck, brief: incomplete}, false},
		{"PLANNING", seed{state: domain.StatePlanning, brief: completeBrief()}, true},
		{"VISUAL_DESIGN without Scenes", seed{state: domain.StateVisualDesign, brief: completeBrief()}, true},
		{"AWAITING_APPROVAL", seed{state: domain.StateAwaitingApproval, brief: completeBrief()}, false},
		{"DIRECTOR mode", seed{mode: domain.ModeDirector, brief: completeBrief()}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			id := f.project(t, tt.seed)

			require.NoError(t, f.orch.Kick(ctx, id))

			if !tt.want {
				assert.Zero(t, f.jobs.count())
				assert.Empty(t, f.chat.texts())
				return
			}
			require.Equal(t, 1, f.jobs.count())
			assert.Equal(t, id, payloadProject(t, f.jobs.payloads[0]))
			require.Len(t, f.chat.announced, 1, "the progress card is posted")
			card := f.chat.announced[0]
			assert.Equal(t, domain.KindProgress, card.Kind)
			assert.Equal(t, "job-1", card.RefID, "the card follows the queued job")
		})
	}
}

func payloadProject(t *testing.T, payload any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	var p struct {
		ProjectID string `json:"project_id"`
	}
	require.NoError(t, json.Unmarshal(raw, &p))
	return p.ProjectID
}

func TestKick_VisualDesignWithScenesHasNothingToDo(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{state: domain.StateVisualDesign, brief: completeBrief()})
	f.saveScript(t, id)
	require.NoError(t, f.plans.SavePlan(context.Background(), id,
		domain.ScenePlan{Scenes: []domain.PlannedScene{{Title: "S", Shots: []domain.PlannedShot{{Objective: "o", DurationSeconds: 3}}}}},
		domain.Storyboard{Shots: [][]domain.ShotStoryboard{{{Description: "f"}}}}))

	require.NoError(t, f.orch.Kick(context.Background(), id))
	assert.Zero(t, f.jobs.count())
}

func TestKick_UnknownProjectIsNotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	err := f.orch.Kick(context.Background(), "missing")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	assert.Zero(t, f.jobs.count())
}

func TestKick_CoalescesKicksWhileAJobIsOnTheProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})

	require.NoError(t, f.orch.Kick(context.Background(), id))
	require.NoError(t, f.orch.Kick(context.Background(), id))
	require.NoError(t, f.orch.Kick(context.Background(), id))

	assert.Equal(t, 1, f.jobs.count(), "one job serves every kick until it ends")
}

func TestKick_ClaimIsReleasedWhenTheJobEnds(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	require.NoError(t, f.orch.Kick(context.Background(), id))
	require.NoError(t, f.handle(context.Background(), id))
	require.Equal(t, domain.StateVisualDesign, f.state(t, id))

	// A Director-mode rollback to PLANNING by the user needs work again: a new job is queued.
	planning := domain.StatePlanning
	require.NoError(t, f.projects.Apply(context.Background(), repository.Turn{ProjectID: id, State: &planning}))
	require.NoError(t, f.orch.Kick(context.Background(), id))
	assert.Equal(t, 2, f.jobs.count())
}

func TestKick_EnqueueFailureReleasesTheClaimAndIsReported(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	f.jobs.err = errors.New("queue is down")

	err := f.orch.Kick(context.Background(), id)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enqueue plan job")
	assert.Contains(t, err.Error(), "queue is down")
	assert.Empty(t, f.chat.texts(), "no progress card for a job that was never queued")

	f.jobs.err = nil
	require.NoError(t, f.orch.Kick(context.Background(), id))
	assert.Equal(t, 1, f.jobs.count(), "the failed attempt did not leave the Project claimed")
}

func TestKick_EnqueueFailureWithAConcurrentKickDoesNotStrandTheClaim(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})

	// Kick A fails to queue its job; Kick B arrives while A is still inside Enqueue and
	// therefore only asks for a rerun. Neither may leave the Project claimed without a job.
	f.jobs.onEnqueue = func() error {
		f.jobs.onEnqueue = nil
		require.NoError(t, f.orch.Kick(context.Background(), id)) // Kick B
		return errors.New("database is locked")
	}
	require.Error(t, f.orch.Kick(context.Background(), id)) // Kick A
	require.Zero(t, f.jobs.count())

	require.NoError(t, f.orch.Kick(context.Background(), id))
	assert.Equal(t, 1, f.jobs.count(), "a later Kick queues the plan job again")
}

func TestKick_AFailingProgressCardDoesNotFailTheKick(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	f.chat.announceFn = func(domain.ChatMessage) error { return errors.New("chat is down") }

	require.NoError(t, f.orch.Kick(context.Background(), id))
	assert.Equal(t, 1, f.jobs.count())
}
