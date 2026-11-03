package orchestrator_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestHandle_FailureLeavesTheStateAndTellsTheUser(t *testing.T) {
	t.Parallel()
	apiErr := func(status int) error {
		return &llm.APIError{Provider: llm.ProviderAnthropic, Status: status, Message: "boom"}
	}
	tests := []struct {
		name      string
		agent     llm.Agent
		err       error
		wantState domain.ProjectState
		wantCalls []llm.Agent
		wantText  string
		wantLogic func(t *testing.T, f *fixture, id string)
	}{
		{
			name: "Producer fails", agent: llm.AgentProducer, err: apiErr(500),
			wantState: domain.StatePlanning, wantCalls: []llm.Agent{llm.AgentProducer},
			wantText: "相手のサービス側で障害が起きています",
			wantLogic: func(t *testing.T, f *fixture, id string) {
				_, err := f.plans.LatestScript(context.Background(), id)
				assert.ErrorIs(t, err, repository.ErrNotFound, "no script is stored")
			},
		},
		{
			name: "Writer fails with a rate limit", agent: llm.AgentWriter, err: apiErr(429),
			wantState: domain.StatePlanning, wantCalls: []llm.Agent{llm.AgentProducer, llm.AgentWriter},
			wantText: "レート制限",
			wantLogic: func(t *testing.T, f *fixture, id string) {
				_, err := f.plans.LatestScript(context.Background(), id)
				assert.ErrorIs(t, err, repository.ErrNotFound, "a half-made script is not stored")
			},
		},
		{
			name: "Writer has no API key", agent: llm.AgentWriter, err: fmt.Errorf("adapter: %w", llm.ErrNoAPIKey),
			wantState: domain.StatePlanning, wantCalls: []llm.Agent{llm.AgentProducer, llm.AgentWriter},
			wantText: "API キーが設定されていません",
		},
		{
			name: "Producer times out", agent: llm.AgentProducer, err: llm.ErrTimeout,
			wantState: domain.StatePlanning, wantCalls: []llm.Agent{llm.AgentProducer},
			wantText: "時間内に返りませんでした",
		},
		{
			name: "Producer's circuit breaker is open", agent: llm.AgentProducer, err: fmt.Errorf("producer: %w", llm.ErrCircuitOpen),
			wantState: domain.StatePlanning, wantCalls: []llm.Agent{llm.AgentProducer},
			wantText: "呼び出しを止めています",
		},
		{
			name: "Director fails", agent: llm.AgentDirector, err: apiErr(503),
			wantState: domain.StateVisualDesign, wantCalls: []llm.Agent{llm.AgentProducer, llm.AgentWriter, llm.AgentDirector},
			wantText: "相手のサービス側で障害が起きています",
			wantLogic: func(t *testing.T, f *fixture, id string) {
				script, err := f.plans.LatestScript(context.Background(), id)
				require.NoError(t, err)
				assert.Equal(t, 1, script.VersionNumber, "the script written before the failure is kept")
				assert.Zero(t, f.sceneCount(t, id))
			},
		},
		{
			name: "Storyboard fails", agent: llm.AgentStoryboard, err: errors.New("unclassified"),
			wantState: domain.StateVisualDesign, wantCalls: []llm.Agent{llm.AgentProducer, llm.AgentWriter, llm.AgentDirector, llm.AgentStoryboard},
			wantText: "原因がわかりませんでした",
			wantLogic: func(t *testing.T, f *fixture, id string) {
				assert.Zero(t, f.sceneCount(t, id), "the plan is saved only with its storyboard")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			id := f.project(t, seed{brief: completeBrief()})
			f.llm.errs[tt.agent] = tt.err

			err := f.handle(context.Background(), id)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantText, "the job is recorded with the user-facing reason")
			assert.NotContains(t, err.Error(), "boom", "provider details stay in the log")
			assert.Equal(t, tt.wantState, f.state(t, id))
			assert.Equal(t, tt.wantCalls, f.llm.called())
			texts := f.chat.texts()
			require.Len(t, texts, 1)
			assert.Contains(t, texts[0], "制作を続けられませんでした")
			assert.Contains(t, texts[0], tt.wantText)
			assert.Equal(t, []domain.MessageKind{domain.KindText}, f.chat.kinds())
			if tt.wantLogic != nil {
				tt.wantLogic(t, f, id)
			}
		})
	}
}

func TestHandle_UnusableModelOutputIsReportedAsSuch(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		agent  llm.Agent
		answer string
	}{
		"not JSON at all":      {llm.AgentProducer, "sorry, I cannot"},
		"concept without plan": {llm.AgentProducer, `{"title":"t","concept":"c","structure":[]}`},
		"empty script":         {llm.AgentWriter, `{"title":"t","scenes":[]}`},
		"empty shot plan":      {llm.AgentDirector, `{"scenes":[]}`},
		"storyboard mismatch":  {llm.AgentStoryboard, `{"scenes":[{"shots":[{"description":"only one"}]}]}`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			id := f.project(t, seed{brief: completeBrief()})
			f.llm.answers[tt.agent] = tt.answer

			err := f.handle(context.Background(), id)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "求めた形式になっていませんでした")
			assert.Zero(t, f.sceneCount(t, id))
			require.Len(t, f.chat.texts(), 1)
		})
	}
}

func TestHandle_AfterAFailureTheNextRunContinuesFromWhereItStopped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	f.llm.errs[llm.AgentDirector] = &llm.APIError{Provider: llm.ProviderAnthropic, Status: 500}
	require.Error(t, f.handle(ctx, id))
	require.Equal(t, domain.StateVisualDesign, f.state(t, id))

	delete(f.llm.errs, llm.AgentDirector)
	require.NoError(t, f.orch.Kick(ctx, id), "the failed job released the claim, so a new job is queued")
	assert.Equal(t, 1, f.jobs.count())
	require.NoError(t, f.handle(ctx, id))

	assert.Equal(t, 1, f.sceneCount(t, id))
	assert.Equal(t, 1, countAgent(f.llm.called(), llm.AgentWriter), "the script was not written again")
	assert.Equal(t, 2, countAgent(f.llm.called(), llm.AgentDirector))
}

func countAgent(calls []llm.Agent, a llm.Agent) int {
	n := 0
	for _, c := range calls {
		if c == a {
			n++
		}
	}
	return n
}

func TestHandle_FailureNoticeFailingStillReturnsTheReason(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	f.llm.errs[llm.AgentProducer] = llm.ErrTimeout
	f.chat.announceFn = func(domain.ChatMessage) error { return errors.New("chat is down") }

	err := f.handle(context.Background(), id)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "時間内に返りませんでした")
}
