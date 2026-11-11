package chatbrief_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// The scenario of #1114, with invented words: the first message already holds the protagonist, the platform, the format and the
// tone; a vague answer follows. No model vendor is called: the planning Agents answer from a fake.
const (
	firstRequest = "架空の30代女性インフルエンサーを主人公にした、TikTok向けの縦型動画を作りたい。テーマは朝のルーティン。成人で、服は着ている設定。落ち着いた雰囲気で"
	protagonist  = "架空の30代女性インフルエンサー"
	vagueAnswer  = "商品ではなく、ただの投稿動画"
)

// newFakeReasoner answers the planning Agents with canned JSON about the protagonist of this scenario (a test can change an answer: SetAnswer).
func newFakeReasoner() *servicetest.FakeReasoner {
	f := servicetest.NewFakeReasoner()
	f.SetAnswer(llm.AgentProducer, `{"title":"朝の一枚","concept":"朝の支度","protagonist":"ミオ（30代の女性インフルエンサー）","structure":[{"name":"フック","description":"d","duration_seconds":5}]}`)
	f.SetAnswer(llm.AgentWriter, `{"title":"脚本","scenes":[{"title":"朝","description":"ミオが起きる"}]}`)
	f.SetAnswer(llm.AgentDirector, `{"scenes":[{"title":"朝","shots":[{"objective":"起きる","duration_seconds":5}]}]}`)
	f.SetAnswer(llm.AgentStoryboard, `{"scenes":[{"shots":[{"description":"ベッド"}]}]}`)
	return f
}

type pipelineFixture struct {
	fixture
	llm *servicetest.FakeReasoner
}

func newPipelineFixture(t *testing.T) pipelineFixture {
	t.Helper()
	testenv.Parallel(t)
	db := testdb.Open(t)
	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
	fake := newFakeReasoner()
	svc := servicetest.New(t, service.Deps{DB: db, Jobs: jobs, LLM: fake, AssetsDir: filepath.Join(t.TempDir(), "assets")})
	ctx, cancel := context.WithCancel(context.Background())
	jobs.Start(ctx)
	t.Cleanup(func() { cancel(); jobs.Stop() })
	return pipelineFixture{fixture: fixture{db: db, svc: svc}, llm: fake}
}

func (f pipelineFixture) create(t *testing.T, content string) domain.Project {
	t.Helper()
	p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: content})
	require.NoError(t, err)
	return p
}

// cards are the question cards of the thread, in order.
func (f pipelineFixture) cards(t *testing.T, projectID string) []domain.ThreadMessage {
	t.Helper()
	ws, err := f.svc.Chat.Workspace(context.Background(), projectID)
	require.NoError(t, err)
	var out []domain.ThreadMessage
	for _, m := range ws.Messages {
		if m.Kind == domain.KindQuestion {
			out = append(out, m)
		}
	}
	return out
}

func (f pipelineFixture) waitProducer(t *testing.T) string {
	t.Helper()
	var got []string
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		got = f.llm.Prompts(llm.AgentProducer)
		assert.NotEmpty(c, got, "the Producer was never asked")
	}, 5*time.Second, 10*time.Millisecond)
	return got[0]
}

func (f pipelineFixture) settle(t *testing.T, projectID string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ws, err := f.svc.Chat.Workspace(context.Background(), projectID)
		require.NoError(c, err)
		for _, m := range ws.Messages {
			if m.Kind == domain.KindProgress {
				assert.True(c, m.Job != nil && (m.Job.Status == domain.JobSucceeded || m.Job.Status == domain.JobFailed), "a plan job is still running")
			}
		}
	}, 5*time.Second, 10*time.Millisecond)
}

// The first message holds the protagonist, the platform, the format and the tone: nothing of it is asked again, the recommended values are
// marked as such, and ONE summary asks for the confirmation. A vague answer afterwards changes nothing, and the planning Agents get the
// original protagonist.
func TestScenario_FirstRequestWithProtagonistThenAVagueAnswer(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.create(t, firstRequest)

	b := f.brief(t, p.ID)
	assert.Equal(t, protagonist, b.Subject)
	assert.Equal(t, "9:16", b.AspectRatio)
	assert.Equal(t, "15秒", b.Duration)
	assert.Equal(t, domain.BriefSourceRecommended, b.SourceOf(domain.BriefDuration), "a proposal, flagged as one")
	assert.Equal(t, domain.BriefSourceInferred, b.SourceOf(domain.BriefAspectRatio), "the user said vertical")
	assert.Contains(t, b.AdditionalInstruction, "制約: 成人・架空・着衣")
	cards := f.cards(t, p.ID)
	require.Len(t, cards, 1, "one summary instead of one question per item")
	assert.Equal(t, domain.RefBriefConfirm, cards[0].RefType)
	assert.Contains(t, domain.DecodeCardBody(cards[0].Content).Text, "Duration: 15秒（推奨・仮の値）")
	assert.True(t, cards[0].Open)

	// A vague answer in free text and as a reply: the protagonist stays, nothing new is asked, the summary stays open.
	f.post(t, p.ID, vagueAnswer, "")
	f.post(t, p.ID, vagueAnswer, cards[0].ID)
	assert.Equal(t, b, f.brief(t, p.ID), "the Brief did not move")
	cards = f.cards(t, p.ID)
	require.Len(t, cards, 2, "the summary is shown again after the reply that did not confirm it")
	assert.False(t, cards[0].Open)
	assert.True(t, cards[1].Open)
	assert.Equal(t, domain.StateDraft, f.state(t, p.ID), "nothing was confirmed")

	// Confirming starts the planning with the original protagonist and the constraints.
	f.post(t, p.ID, briefing.ConfirmOption, cards[1].ID)
	prompt := f.waitProducer(t)
	assert.Contains(t, prompt, "固定の制約: 主人公・題材は「"+protagonist+"」")
	assert.Contains(t, prompt, "制約: 成人・架空・着衣")
	assert.NotContains(t, prompt, "仮の値", "the confirmed values are the user's")
	f.settle(t, p.ID)
	require.NotEmpty(t, f.llm.Prompts(llm.AgentWriter))
	assert.Contains(t, f.llm.Prompts(llm.AgentWriter)[0], protagonist)
	assert.Contains(t, f.llm.Prompts(llm.AgentWriter)[0], "ミオ（30代の女性インフルエンサー）", "the Writer reads the protagonist of the Concept")
}

// FULL AUTO adds no confirmation loop: switching to it while the summary waits starts the planning, the recommended values are adopted as
// tentative values, and nothing is asked.
func TestScenario_FullAutoAdoptsTheRecommendationsWithoutAsking(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.create(t, firstRequest)
	require.Zero(t, len(f.llm.Prompts(llm.AgentProducer)), "KEY CHECK waits for the summary")

	_, err := f.svc.Projects.SetConfirmationLevel(context.Background(), p.ID, domain.ConfirmFullAuto, nil)
	require.NoError(t, err)

	prompt := f.waitProducer(t)
	assert.Contains(t, prompt, "主人公・題材は「"+protagonist+"」")
	assert.Contains(t, prompt, "仮の値（目的から決めた推奨値。ユーザーが明示した指定ではない）: Duration", "adopted, and still known to be a proposal")
	assert.Len(t, f.cards(t, p.ID), 1, "no further question or summary was posted")
	assert.True(t, f.brief(t, p.ID).IsDelegated(domain.BriefTarget), "what cannot be inferred is left to the AI")
	f.settle(t, p.ID)
}

// FULL AUTO stops for the one thing that cannot be decided: the Subject. Answering it starts the planning with no summary.
func TestScenario_FullAutoStopsOnlyForTheSubject(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.create(t, "TikTok向けの縦型動画を作りたい")
	_, err := f.svc.Projects.SetConfirmationLevel(context.Background(), p.ID, domain.ConfirmFullAuto, nil)
	require.NoError(t, err)
	cards := f.cards(t, p.ID)
	require.Len(t, cards, 1)
	assert.Equal(t, string(domain.BriefSubject), cards[0].RefID)
	assert.Empty(t, f.llm.Prompts(llm.AgentProducer), "no script is written about an unnamed topic")

	ws := f.post(t, p.ID, "猫の日常", cards[0].ID)

	assert.Contains(t, f.waitProducer(t), "主人公・題材は「猫の日常」")
	assert.Equal(t, domain.KindText, ws.Messages[len(ws.Messages)-1].Kind, "FULL AUTO says what is adopted and asks nothing")
	assert.Len(t, f.cards(t, p.ID), 1, "no summary after the Subject answer")
	f.settle(t, p.ID)
}

// A platform the table does not know gets no proposal: Duration and Aspect Ratio stay open and the one summary says so.
func TestScenario_UnknownPlatformGetsNoDurationOrRatio(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.create(t, "美容液のCMを作りたい。Instagram広告に出す")

	b := f.brief(t, p.ID)
	assert.Empty(t, b.Duration)
	assert.Empty(t, b.AspectRatio)
	assert.Empty(t, b.Sources[string(domain.BriefDuration)], "no value, so no source")
	text := domain.DecodeCardBody(f.cards(t, p.ID)[0].Content).Text
	assert.Contains(t, text, "Duration: 未設定")
	assert.Contains(t, text, "Aspect Ratio: 未設定")
	assert.NotContains(t, text, "推奨・仮の値", "nothing was proposed, so nothing is marked")
}

func TestScenario_ExplicitChangeReplacesTheSubjectAndTheSummaryIsShownAgain(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.create(t, firstRequest)

	f.post(t, p.ID, "主人公を猫に変更して", "")

	b := f.brief(t, p.ID)
	assert.Equal(t, "猫", b.Subject)
	assert.Equal(t, domain.BriefSourceUser, b.SourceOf(domain.BriefSubject))
	cards := f.cards(t, p.ID)
	require.Len(t, cards, 2)
	assert.Contains(t, domain.DecodeCardBody(cards[1].Content).Text, "Subject: 猫")
}

// A Subject that differs without saying it is a change is a contradiction: only that point is asked, with the two values as buttons.
func TestScenario_ContradictionAsksOnlyAboutTheSubject(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.create(t, firstRequest)

	f.post(t, p.ID, "ロボットを主人公にした動画も作りたい", "")

	assert.Equal(t, protagonist, f.brief(t, p.ID).Subject, "the Subject is kept until the user picks")
	cards := f.cards(t, p.ID)
	require.Len(t, cards, 2)
	q := cards[1]
	assert.Equal(t, string(domain.BriefSubject), q.RefID)
	assert.True(t, q.Open)
	assert.Equal(t, []string{protagonist, "ロボット"}, domain.DecodeCardBody(q.Content).Options)
	assert.Equal(t, domain.StateDraft, f.state(t, p.ID))

	// A remark that names neither value does not settle it; a pick does, and the summary follows.
	f.post(t, p.ID, vagueAnswer, q.ID)
	assert.Equal(t, protagonist, f.brief(t, p.ID).Subject)
	f.post(t, p.ID, protagonist, f.lastMessage(t, p.ID).ID)
	b := f.brief(t, p.ID)
	assert.Equal(t, protagonist, b.Subject)
	assert.Empty(t, b.ProposedSubject)
	cards = f.cards(t, p.ID)
	assert.Equal(t, domain.RefBriefConfirm, cards[len(cards)-1].RefType)
}

// The guard: a plan whose protagonist clearly contradicts the Brief is not stored, and the script is not written.
func TestScenario_APlanAboutAnotherProtagonistIsNotUsed(t *testing.T) {
	f := newPipelineFixture(t)
	f.llm.SetAnswer(llm.AgentProducer, `{"title":"t","concept":"c","protagonist":"30代の男性会社員","structure":[{"name":"a","description":"d"}]}`)
	p := f.create(t, firstRequest)
	cards := f.cards(t, p.ID)

	f.post(t, p.ID, briefing.ConfirmOption, cards[0].ID)

	f.waitProducer(t)
	f.settle(t, p.ID)
	assert.Empty(t, f.llm.Prompts(llm.AgentWriter), "the Writer never runs on a plan about someone else")
	require.NotEmpty(t, f.state(t, p.ID))
	assert.Equal(t, domain.StatePlanning, f.state(t, p.ID))
	ws, err := f.svc.Chat.Workspace(context.Background(), p.ID)
	require.NoError(t, err)
	var failed bool
	for _, m := range ws.Messages {
		failed = failed || (m.Kind == domain.KindText && strings.Contains(m.Content, "制作を続けられませんでした"))
	}
	assert.True(t, failed, "the failure is told in the thread")
}

// Once the planning started the summary is not a question any more, whichever way it started.
func TestScenario_TheSummaryClosesWhenFullAutoStartsThePlanning(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.create(t, firstRequest)
	require.True(t, f.cards(t, p.ID)[0].Open)

	_, err := f.svc.Projects.SetConfirmationLevel(context.Background(), p.ID, domain.ConfirmFullAuto, nil)
	require.NoError(t, err)
	f.waitProducer(t)
	f.settle(t, p.ID)

	assert.False(t, f.cards(t, p.ID)[0].Open)
}

// A text that states an item is taken for it, and is not also stored as the answer to the question that happens to be open.
func TestScenario_FreeTextThatStatesAnItemIsNotTheSubjectAnswer(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.create(t, "TikTok向けの縦型動画を作りたい")
	require.Equal(t, string(domain.BriefSubject), f.cards(t, p.ID)[0].RefID)

	f.post(t, p.ID, "30秒でお願いします", "")

	b := f.brief(t, p.ID)
	assert.Equal(t, "30秒", b.Duration)
	assert.Equal(t, domain.BriefSourceInferred, b.SourceOf(domain.BriefDuration), "stated, replacing the proposal")
	assert.Empty(t, b.Subject)
}
