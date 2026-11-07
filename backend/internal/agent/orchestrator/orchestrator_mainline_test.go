package orchestrator_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// subjectLessBrief is a Brief whose Subject the user never named (it was left to the AI, as the chat once allowed).
func subjectLessBrief() domain.Brief {
	b := completeBrief()
	b.Subject = ""
	b.Delegate(domain.BriefSubject)
	return b
}

// #1108: a Project that has no Subject never gets a script about a topic nobody named: the Producer and the Writer do not run, and the
// user is asked (a Subject question card), once.
func TestHandle_NoSubjectAsksInsteadOfWritingAScript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{state: domain.StatePlanning, brief: subjectLessBrief()})

	require.NoError(t, f.handle(ctx, id))

	assert.Empty(t, f.llm.called(), "no Agent writes about an unnamed topic")
	_, err := f.plans.LatestScript(ctx, id)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	assert.Equal(t, domain.StatePlanning, f.state(t, id))
	require.Len(t, f.chat.announced, 1)
	q := f.chat.announced[0]
	assert.Equal(t, domain.KindQuestion, q.Kind)
	assert.Equal(t, domain.RefBriefField, q.RefType)
	assert.Equal(t, string(domain.BriefSubject), q.RefID)
	body := domain.DecodeCardBody(q.Content)
	assert.Contains(t, body.Text, "Subject（題材）がまだ決まっていません")
	assert.Contains(t, body.Text, "脚本は作りません")

	// The card is stored; while nobody answered it, running again asks nothing more.
	require.NoError(t, f.projects.Apply(ctx, repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{q}}))
	require.NoError(t, f.handle(ctx, id))
	assert.Len(t, f.chat.announced, 1, "an open Subject question is not asked again")
	assert.Empty(t, f.llm.called())
}

func TestHandle_SubjectAnsweredLaterLetsTheScriptBeWritten(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	id := f.project(t, seed{state: domain.StatePlanning, brief: subjectLessBrief()})
	require.NoError(t, f.handle(ctx, id))
	require.Empty(t, f.llm.called())

	b, err := f.projects.GetBrief(ctx, id)
	require.NoError(t, err)
	b.Set(domain.BriefSubject, "美容液")
	require.NoError(t, f.projects.Apply(ctx, repository.Turn{ProjectID: id, Brief: &b}))
	require.NoError(t, f.handle(ctx, id))

	assert.Equal(t, []llm.Agent{llm.AgentProducer, llm.AgentWriter, llm.AgentDirector, llm.AgentStoryboard}, f.llm.called())
	assert.Equal(t, domain.StateVisualDesign, f.state(t, id))
	assert.Contains(t, strings.Join(f.llm.promptsOf(llm.AgentProducer), "\n"), "美容液")
}

// The user's own words about what the video is (Additional Instruction) are a topic: the script is written, and the prompt does not
// call the Subject "left to the AI".
func TestHandle_AdditionalInstructionIsATopicWhenTheSubjectIsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	b := subjectLessBrief()
	b.AdditionalInstruction = "夜の街を走る自転車の動画"
	id := f.project(t, seed{state: domain.StatePlanning, brief: b})

	require.NoError(t, f.handle(ctx, id))

	assert.Equal(t, domain.StateVisualDesign, f.state(t, id))
	prompt := strings.Join(f.llm.promptsOf(llm.AgentProducer), "\n")
	assert.Contains(t, prompt, "夜の街を走る自転車の動画")
	assert.NotContains(t, prompt, "Subject: （AI に任せる")
	assert.Contains(t, prompt, "Brief に無い題材を作らない")
}

// A DRAFT Project whose Subject is not decided does not move to PLANNING.
func TestKick_DraftWithoutSubjectDoesNotStart(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: subjectLessBrief()})

	require.NoError(t, f.orch.Kick(context.Background(), id))

	assert.Zero(t, f.jobs.count())
	assert.Equal(t, domain.StateDraft, f.state(t, id))
}

// #1108: after the Scene / Shot plan, the Project stands in VISUAL_DESIGN with every Shot PLANNING. That is deliberate (a generation costs money and
// is started per Shot), so the reason must be said, with what is missing and what to press next. The Confirmation Level (KEY CHECK) is not the
// reason, and the approval and budget gates are not bypassed: no generation job is queued.
func TestHandle_PlanNoticeSaysWhyNothingGeneratesAndWhatToPressNext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFixture(t)
	f.orch = orchestrator.New(orchestrator.Deps{
		Projects: f.projects, Plans: f.plans, ChatRepo: f.chatRepo, Chat: f.chat, Jobs: f.jobs, LLM: f.llm, Notes: f.notes,
		Facts: func(context.Context, string) (int, int, error) { return 0, 0, nil },
	})
	id := f.project(t, seed{brief: completeBrief(), level: domain.ConfirmKeyCheck})
	require.NoError(t, f.handle(ctx, id))
	require.Equal(t, domain.StateAwaitingApproval, f.state(t, id))
	script, err := f.plans.LatestScript(ctx, id)
	require.NoError(t, err)
	card := f.scriptCard(t, id, script.ID)
	require.NotNil(t, card)
	f.answer(t, id, card, domain.ApprovalApproved, "")

	require.NoError(t, f.handle(ctx, id))

	assert.Equal(t, domain.StateVisualDesign, f.state(t, id))
	scenes, err := f.plans.ListScenes(ctx, id)
	require.NoError(t, err)
	require.NotEmpty(t, scenes)
	shots, err := f.plans.ListShots(ctx, scenes[0].ID)
	require.NoError(t, err)
	assert.Len(t, shots, 3, "every Shot is PLANNING with no Take")

	texts := f.chat.texts()
	notice := texts[len(texts)-1]
	assert.Contains(t, notice, "Scene / Shot の計画ができました")
	assert.Contains(t, notice, "Shot 3 件が PLANNING")
	assert.Contains(t, notice, "生成の開始を待っています")
	assert.Contains(t, notice, "「生成の承認」は出さず", "without the generation gate (DIRECTOR / MANUAL, or no generation wired) nothing is asked and the user starts each Shot")
	assert.Contains(t, notice, "Reference Library が 0 件")
	assert.Contains(t, notice, "Character）が 0 人")
	assert.Contains(t, notice, "次に押すもの")
	assert.Contains(t, notice, "「生成」")
	assert.Zero(t, f.jobs.count(), "the gates stay: no generation is queued behind the user's back")
}
