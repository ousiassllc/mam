package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestHandle_ShotPlanWithoutAScriptStopsBeforeAnyAgentRuns(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{state: domain.StateVisualDesign, brief: completeBrief()})

	err := f.handle(context.Background(), id)

	require.Error(t, err)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	assert.Contains(t, err.Error(), "no script for the Scene plan")
	assert.Empty(t, f.chat.texts(), "a missing row is reported like a vanished Project: nobody is told")
	assert.Empty(t, f.llm.called())
	assert.Zero(t, f.sceneCount(t, id))
}

func TestHandle_CancelledContextStopsBeforeAnyAgentRunsAndStaysQuiet(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := f.handle(ctx, id)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, f.llm.called())
	assert.Empty(t, f.chat.texts(), "on shutdown nobody is told: the job is queued again")
	assert.Equal(t, domain.StateDraft, f.state(t, id))
}

func TestHandle_CancelDuringAnAgentCallKeepsThePersistedStateAndStaysQuiet(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.llm.onCall = func(agent llm.Agent) error {
		if agent == llm.AgentWriter {
			cancel()
			return ctx.Err()
		}
		return nil
	}

	err := f.handle(ctx, id)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []llm.Agent{llm.AgentProducer, llm.AgentWriter}, f.llm.called())
	assert.Equal(t, domain.StatePlanning, f.state(t, id), "the move to PLANNING was persisted before the Agents ran")
	_, serr := f.plans.LatestScript(context.Background(), id)
	assert.ErrorIs(t, serr, repository.ErrNotFound)
	assert.Empty(t, f.chat.texts())

	// The claim is released: the next kick queues a fresh job, and running it finishes the work.
	require.NoError(t, f.orch.Kick(context.Background(), id))
	assert.Equal(t, 1, f.jobs.count())
	f.llm.onCall = nil
	require.NoError(t, f.handle(context.Background(), id))
	assert.Equal(t, domain.StateVisualDesign, f.state(t, id))
}

func TestHandle_ApprovalRequestFailureIsReported(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief(), level: domain.ConfirmKeyCheck})
	f.chat.approvalFn = func() error { return errors.New("db locked") }

	err := f.handle(context.Background(), id)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "原因がわかりませんでした")
	assert.Equal(t, domain.StatePlanning, f.state(t, id))
	script, serr := f.plans.LatestScript(context.Background(), id)
	require.NoError(t, serr, "the script is kept so the next run only asks for approval")
	assert.Equal(t, 1, script.VersionNumber)
}

func TestHandle_ADecisionThatArrivesWhileRunningIsNotMissed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	require.NoError(t, f.orch.Kick(ctx, id))
	require.Equal(t, 1, f.jobs.count())

	// Right after the shot plan is announced, the last thing the advance loop does, the user
	// sends the Project back to PLANNING and kicks. The running job must look again.
	var kicked bool
	f.chat.announceFn = func(m domain.ChatMessage) error {
		if m.Kind != domain.KindText || kicked {
			return nil
		}
		kicked = true
		back := domain.StatePlanning
		require.NoError(t, f.projects.Apply(ctx, repository.Turn{ProjectID: id, State: &back}))
		require.NoError(t, f.orch.Kick(ctx, id))
		return nil
	}

	require.NoError(t, f.handle(ctx, id))

	require.True(t, kicked)
	assert.Equal(t, 1, f.jobs.count(), "the kick joined the running job instead of queuing another")
	assert.Equal(t, domain.StateVisualDesign, f.state(t, id), "the job advanced the Project again after the decision")
	assert.Len(t, f.llm.called(), 4, "the existing script and plan are reused")
}

func TestHandle_WritersAndDirectorsGetTheProjectNotes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	_, err := f.notes.Create(ctx, repository.NewNote{
		ProjectID: id, Category: domain.NoteCategoryWorld, Pinned: true,
		Title: "秘密の設定", Content: "過去の火事の話は序盤では出さない", Source: domain.NoteSourceManual,
	})
	require.NoError(t, err)

	require.NoError(t, f.handle(ctx, id))

	for _, agent := range []llm.Agent{llm.AgentWriter, llm.AgentDirector} {
		prompts := f.llm.promptsOf(agent)
		require.Len(t, prompts, 1)
		assert.Contains(t, prompts[0], "過去の火事の話は序盤では出さない", string(agent))
	}
	assert.NotContains(t, f.llm.promptsOf(llm.AgentProducer)[0], "過去の火事の話は序盤では出さない", "the Producer is not given notes")
}

func TestHandle_NotesReadFailureStopsThePipeline(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	require.NoError(t, f.db.Close())

	err := f.handle(context.Background(), id)
	require.Error(t, err)
	assert.NotErrorIs(t, err, planning.ErrInvalidPlan)
	assert.Empty(t, f.llm.called())
}

func TestHandle_SameProjectRunsOneAdvanceAtATime(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	f.llm.onCall = func(agent llm.Agent) error {
		if agent == llm.AgentProducer {
			once.Do(func() { close(entered) })
			<-release
		}
		return nil
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(1)
	go func() { defer wg.Done(); errs[0] = f.handle(context.Background(), id) }()
	<-entered
	wg.Add(1)
	go func() { defer wg.Done(); errs[1] = f.handle(context.Background(), id) }()
	close(release)
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	assert.Equal(t, 1, countAgent(f.llm.called(), llm.AgentProducer), "the second job found the work done instead of repeating it")
	assert.Equal(t, 1, f.sceneCount(t, id))
	assert.Equal(t, 1, strings.Count(strings.Join(f.chat.texts(), "\n"), "Scene / Shot の計画ができました"))
}
