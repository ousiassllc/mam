package orchestrator_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// protagonistBrief is a Brief read from a first request that named the protagonist, with a recommended Duration and nothing confirmed.
func protagonistBrief() domain.Brief {
	return domain.Brief{
		ContentType: "SNS/ショート動画", Objective: "TikTok", Subject: "架空の30代女性インフルエンサー", Duration: "15秒", AspectRatio: "9:16",
		AdditionalInstruction: "制約: 成人・架空・着衣",
		Sources: map[string]string{
			string(domain.BriefSubject): domain.BriefSourceInferred, string(domain.BriefObjective): domain.BriefSourceInferred,
			string(domain.BriefDuration): domain.BriefSourceRecommended, string(domain.BriefAspectRatio): domain.BriefSourceInferred,
		},
	}
}

// #1114: below FULL AUTO a Brief that holds proposals waits for the confirmation; the pipeline does not start on it.
func TestHandle_UnconfirmedBriefWaitsBelowFullAuto(t *testing.T) {
	t.Parallel()
	for _, level := range []domain.ConfirmationLevel{domain.ConfirmKeyCheck, domain.ConfirmCustomCheck} {
		t.Run(string(level), func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			id := f.project(t, seed{level: level, brief: protagonistBrief()})

			require.NoError(t, f.orch.Kick(context.Background(), id))
			require.NoError(t, f.handle(context.Background(), id))

			assert.Zero(t, f.jobs.count(), "no plan job")
			assert.Equal(t, domain.StateDraft, f.state(t, id))
			assert.Empty(t, f.llm.called())
		})
	}
}

// At FULL AUTO the same Brief starts at once: no confirmation, the open items are left to the AI, the proposal is adopted and still
// told to the Agents as a proposal, and the protagonist is a fixed condition of the prompt.
func TestHandle_FullAutoAdoptsTheProposalsAndKeepsTheProtagonist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{level: domain.ConfirmFullAuto, brief: protagonistBrief()})

	require.NoError(t, f.handle(ctx, id))

	assert.Equal(t, domain.StateVisualDesign, f.state(t, id))
	for _, k := range f.chat.kinds() {
		assert.NotEqual(t, domain.KindQuestion, k, "nothing was asked")
	}
	b, err := f.projects.GetBrief(ctx, id)
	require.NoError(t, err)
	assert.True(t, b.IsDelegated(domain.BriefTarget), "what could not be inferred is left to the AI")
	assert.Equal(t, domain.BriefSourceRecommended, b.SourceOf(domain.BriefDuration), "adopting does not make the proposal the user's")
	prompt := strings.Join(f.llm.promptsOf(llm.AgentProducer), "\n")
	assert.Contains(t, prompt, "固定の制約: 主人公・題材は「架空の30代女性インフルエンサー」")
	assert.Contains(t, prompt, "制約: 成人・架空・着衣")
	assert.Contains(t, prompt, "仮の値（目的から決めた推奨値。ユーザーが明示した指定ではない）: Duration")
	for _, agent := range []llm.Agent{llm.AgentWriter, llm.AgentDirector, llm.AgentStoryboard} {
		assert.Contains(t, strings.Join(f.llm.promptsOf(agent), "\n"), "架空の30代女性インフルエンサー", "the %s is told the protagonist too", agent)
	}
}

// FULL AUTO still stops where nothing can be decided: a Subject in doubt (contradicted) holds the start, whatever the level.
func TestHandle_FullAutoStopsOnAContradictedSubject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	b := protagonistBrief()
	b.ProposedSubject = "ロボット"
	id := f.project(t, seed{level: domain.ConfirmFullAuto, brief: b})

	require.NoError(t, f.handle(context.Background(), id))

	assert.Equal(t, domain.StateDraft, f.state(t, id))
	assert.Empty(t, f.llm.called())
}

// A confirmed Brief (every value the user's) starts below FULL AUTO without any question: the first thing it waits for is the script.
func TestHandle_ConfirmedBriefStartsAtKeyCheck(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	b := protagonistBrief()
	b.Sources = nil
	b.Delegate(domain.BriefTarget)
	b.Delegate(domain.BriefMood)
	id := f.project(t, seed{level: domain.ConfirmKeyCheck, brief: b})

	require.NoError(t, f.handle(context.Background(), id))

	assert.Equal(t, domain.StateAwaitingApproval, f.state(t, id), "the script checkpoint, not the Brief")
	assert.Equal(t, []llm.Agent{llm.AgentProducer, llm.AgentWriter}, f.llm.called())
}
