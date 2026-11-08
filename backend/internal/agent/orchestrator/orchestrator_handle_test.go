package orchestrator_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestHandle_FullAutoRunsDraftThroughToTheShotPlan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief(), level: domain.ConfirmFullAuto})

	require.NoError(t, f.handle(ctx, id))

	assert.Equal(t, domain.StateVisualDesign, f.state(t, id))
	assert.Equal(t, []llm.Agent{llm.AgentProducer, llm.AgentWriter, llm.AgentDirector, llm.AgentStoryboard}, f.llm.called())

	script, err := f.plans.LatestScript(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 1, script.VersionNumber)
	assert.Equal(t, "脚本タイトル", script.Doc.Script.Title)
	assert.Equal(t, "朝の一滴", script.Doc.Concept.Title)
	assert.Nil(t, f.scriptCard(t, id, script.ID), "FULL AUTO asks nobody to approve the script")

	scenes, err := f.plans.ListScenes(ctx, id)
	require.NoError(t, err)
	require.Len(t, scenes, 1)
	shots, err := f.plans.ListShots(ctx, scenes[0].ID)
	require.NoError(t, err)
	assert.Len(t, shots, 3)

	texts := f.chat.texts()
	require.Len(t, texts, 1)
	assert.Contains(t, texts[0], "1 Scene / 3 Shot")
}

func TestHandle_KeyCheckWaitsForScriptApproval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief(), level: domain.ConfirmKeyCheck})

	require.NoError(t, f.handle(ctx, id))

	assert.Equal(t, domain.StateAwaitingApproval, f.state(t, id))
	assert.Equal(t, []llm.Agent{llm.AgentProducer, llm.AgentWriter}, f.llm.called(), "nothing runs past the checkpoint")
	script, err := f.plans.LatestScript(ctx, id)
	require.NoError(t, err)
	card := f.scriptCard(t, id, script.ID)
	require.NotNil(t, card)
	assert.Equal(t, domain.ApprovalPending, card.ApprovalStatus)
	assert.Contains(t, card.Content, "脚本 v1「脚本タイトル」（2 Scene / 15秒）", "the card summarises the script")
	assert.Zero(t, f.sceneCount(t, id))

	// Running the job again while the card is unanswered changes nothing.
	require.NoError(t, f.handle(ctx, id))
	assert.Equal(t, 1, f.chat.approvals)
	assert.Len(t, f.llm.called(), 2)
	assert.Equal(t, domain.StateAwaitingApproval, f.state(t, id))
}

func TestHandle_ApprovedScriptMovesOnToTheShotPlan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief(), level: domain.ConfirmKeyCheck})
	require.NoError(t, f.handle(ctx, id))
	script, err := f.plans.LatestScript(ctx, id)
	require.NoError(t, err)

	f.answer(t, id, f.scriptCard(t, id, script.ID), domain.ApprovalApproved, "")
	require.Equal(t, domain.StatePlanning, f.state(t, id))
	require.NoError(t, f.handle(ctx, id))

	assert.Equal(t, domain.StateVisualDesign, f.state(t, id))
	assert.Equal(t, 1, f.sceneCount(t, id))
	assert.Equal(t, []llm.Agent{llm.AgentProducer, llm.AgentWriter, llm.AgentDirector, llm.AgentStoryboard}, f.llm.called(),
		"the approved script is not rewritten")
	assert.Equal(t, 1, f.chat.approvals, "the approved script is not asked again")
}

func TestHandle_ChangesRequestedRewritesTheScriptWithTheFeedback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief(), level: domain.ConfirmKeyCheck})
	require.NoError(t, f.handle(ctx, id))
	v1, err := f.plans.LatestScript(ctx, id)
	require.NoError(t, err)

	f.llm.answers[llm.AgentWriter] = `{"title":"改訂版","scenes":[{"title":"一幕","description":"d"}]}`
	f.answer(t, id, f.scriptCard(t, id, v1.ID), domain.ApprovalChangesRequested, "もっと短くして")
	require.NoError(t, f.handle(ctx, id))

	v2, err := f.plans.LatestScript(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 2, v2.VersionNumber)
	assert.Equal(t, "改訂版", v2.Doc.Script.Title)
	assert.Equal(t, []llm.Agent{llm.AgentProducer, llm.AgentWriter, llm.AgentProducer, llm.AgentWriter}, f.llm.called())

	producerPrompts := f.llm.promptsOf(llm.AgentProducer)
	writerPrompts := f.llm.promptsOf(llm.AgentWriter)
	assert.Contains(t, producerPrompts[1], "もっと短くして", "the change request reaches the Producer")
	assert.Contains(t, producerPrompts[1], "朝の一滴", "the previous concept is kept as the base")
	assert.Contains(t, writerPrompts[1], "もっと短くして", "the change request reaches the Writer")
	assert.Contains(t, writerPrompts[1], "脚本タイトル", "the previous script is kept as the base")
	assert.NotContains(t, producerPrompts[0], "もっと短くして")

	// The new version gets its own approval card and the Project waits again.
	assert.Equal(t, domain.StateAwaitingApproval, f.state(t, id))
	card2 := f.scriptCard(t, id, v2.ID)
	require.NotNil(t, card2)
	assert.Equal(t, domain.ApprovalPending, card2.ApprovalStatus)
	assert.Zero(t, f.sceneCount(t, id))
}

func TestHandle_PendingCardInPlanningKeepsWaiting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{state: domain.StatePlanning, brief: completeBrief(), level: domain.ConfirmKeyCheck})
	v := f.saveScript(t, id)
	require.NoError(t, f.projects.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{{
		Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: "脚本 v1",
		RefType: domain.CheckpointScript.RefType(), RefID: v.ID, ApprovalStatus: domain.ApprovalPending,
	}}}))

	require.NoError(t, f.handle(ctx, id))

	assert.Equal(t, domain.StatePlanning, f.state(t, id))
	assert.Empty(t, f.llm.called())
	assert.Zero(t, f.chat.approvals)
}

func TestHandle_ResumesFromStoredScriptWithoutWritingItAgain(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{state: domain.StatePlanning, brief: completeBrief()})
	f.saveScript(t, id)

	require.NoError(t, f.handle(context.Background(), id))

	assert.Equal(t, []llm.Agent{llm.AgentDirector, llm.AgentStoryboard}, f.llm.called(), "an interrupted run continues after the script")
	assert.Equal(t, domain.StateVisualDesign, f.state(t, id))
	assert.Equal(t, 1, f.sceneCount(t, id))
}

func TestHandle_VisualDesignWithScenesIsANoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{state: domain.StateVisualDesign, brief: completeBrief()})
	f.saveScript(t, id)
	require.NoError(t, f.plans.SavePlan(ctx, id,
		domain.ScenePlan{Scenes: []domain.PlannedScene{{Title: "Keep", Shots: []domain.PlannedShot{{Objective: "o", DurationSeconds: 3}}}}},
		domain.Storyboard{Shots: [][]domain.ShotStoryboard{{{Description: "f"}}}}))

	require.NoError(t, f.handle(ctx, id))

	assert.Empty(t, f.llm.called())
	assert.Empty(t, f.chat.texts())
	scenes, err := f.plans.ListScenes(ctx, id)
	require.NoError(t, err)
	require.Len(t, scenes, 1)
	assert.Equal(t, "Keep", scenes[0].Title)
}

// Below FULL AUTO the Brief must be confirmed (#1114): an item nobody decided is settled by the user before PLANNING starts.
func TestHandle_DraftWithAMissingBriefItemDoesNotStart(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{level: domain.ConfirmKeyCheck, brief: domain.Brief{Subject: "美容液"}})

	require.NoError(t, f.handle(context.Background(), id))

	assert.Equal(t, domain.StateDraft, f.state(t, id))
	assert.Empty(t, f.llm.called())
}

func TestHandle_DirectorModeLeavesTheProjectToTheUser(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{state: domain.StatePlanning, mode: domain.ModeDirector, brief: completeBrief()})

	require.NoError(t, f.handle(context.Background(), id))

	assert.Equal(t, domain.StatePlanning, f.state(t, id))
	assert.Empty(t, f.llm.called())
}

func TestHandle_RejectsAnUnusablePayload(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for name, payload := range map[string]string{"not json": `nope`, "no project": `{}`, "empty project": `{"project_id":""}`} {
		err := f.orch.Handle(context.Background(), domain.Job{ID: "j9", Payload: json.RawMessage(payload)})
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "invalid payload", name)
	}
	assert.Empty(t, f.chat.texts())
}

func TestHandle_UnknownProjectIsNotFoundAndNobodyIsTold(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	err := f.handle(context.Background(), "gone")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	assert.Empty(t, f.chat.texts())
}
