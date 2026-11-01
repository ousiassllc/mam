package service_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// fakeReasoner stands in for the Reasoning LLM Adapter: no external API is called. It answers
// per Agent with canned JSON, can fail an Agent on demand and records every call in order.
type fakeReasoner struct {
	mu    sync.Mutex
	calls []llm.Agent
	users map[llm.Agent][]string
	fail  map[llm.Agent]error
}

func newFakeReasoner() *fakeReasoner {
	return &fakeReasoner{users: map[llm.Agent][]string{}, fail: map[llm.Agent]error{}}
}

const (
	fakeConcept = `{"title":"朝の一滴","logline":"肌が目覚める朝","concept":"朝の光と美容液","tone":"上品","structure":[{"name":"フック","description":"光","duration_seconds":5}]}`
	fakeScript  = `{"title":"脚本","scenes":[{"title":"朝","location":"寝室","description":"光が差す"},{"title":"使う","description":"美容液を手に取る"}]}`
	fakePlan    = `{"scenes":[
		{"title":"朝","location":"寝室","shots":[{"objective":"窓の光","duration_seconds":5},{"objective":"目覚め","duration_seconds":5}]},
		{"title":"使う","shots":[{"objective":"美容液","duration_seconds":5}]}]}`
	fakeBoard = `{"scenes":[{"shots":[{"description":"窓辺の光","framing":"ワイド"},{"description":"目を開ける"}]},{"shots":[{"description":"手のひらの美容液"}]}]}`
)

func (f *fakeReasoner) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, agent)
	f.users[agent] = append(f.users[agent], req.User)
	if err := f.fail[agent]; err != nil {
		return llm.Response{}, err
	}
	text := map[llm.Agent]string{
		llm.AgentProducer: fakeConcept, llm.AgentWriter: fakeScript, llm.AgentDirector: fakePlan, llm.AgentStoryboard: fakeBoard,
	}[agent]
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

func (f *fakeReasoner) agents() []llm.Agent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]llm.Agent(nil), f.calls...)
}

func (f *fakeReasoner) count(agent llm.Agent) int {
	n := 0
	for _, a := range f.agents() {
		if a == agent {
			n++
		}
	}
	return n
}

func (f *fakeReasoner) lastUser(agent llm.Agent) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.users[agent]
	if len(u) == 0 {
		return ""
	}
	return u[len(u)-1]
}

func (f *fakeReasoner) setFailure(agent llm.Agent, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[agent] = err
}

type pipelineFixture struct {
	fixture
	llm *fakeReasoner
}

func newPipelineFixture(t *testing.T) pipelineFixture {
	t.Helper()
	testenv.Parallel(t)
	db := testdb.Open(t)
	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
	fake := newFakeReasoner()
	assets := filepath.Join(t.TempDir(), "assets")
	svc := servicetest.New(t, service.Deps{DB: db, Jobs: jobs, LLM: fake, AssetsDir: assets})
	ctx, cancel := context.WithCancel(context.Background())
	jobs.Start(ctx)
	t.Cleanup(func() { cancel(); jobs.Stop() })
	return pipelineFixture{fixture: fixture{svc: svc, db: db, assetsDir: assets}, llm: fake}
}

// completeBrief fills every Brief item through the Brief form, which starts the pipeline.
func (f pipelineFixture) completeBrief(t *testing.T, projectID string) {
	t.Helper()
	_, err := f.svc.Projects.UpdateBrief(context.Background(), projectID, service.BriefForm{Values: map[domain.BriefField]string{
		domain.BriefContentType: "商品CM", domain.BriefObjective: "Instagram広告", domain.BriefSubject: "美容液",
		domain.BriefTarget: "20代女性", domain.BriefDuration: "15秒", domain.BriefAspectRatio: "9:16", domain.BriefMood: "上品",
	}})
	require.NoError(t, err)
}

func (f pipelineFixture) state(t require.TestingT, projectID string) domain.ProjectState {
	p, err := f.svc.Projects.Get(context.Background(), projectID)
	require.NoError(t, err)
	return p.State
}

func (f pipelineFixture) waitState(t *testing.T, projectID string, want domain.ProjectState) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		p, err := f.svc.Projects.Get(context.Background(), projectID)
		return err == nil && p.State == want
	}, 5*time.Second, 10*time.Millisecond, "the Project never reached %s (now %s)", want, f.state(t, projectID))
}

// approvalCard returns the newest Approval card of the thread, or nil.
func (f pipelineFixture) approvalCard(t require.TestingT, projectID string) *domain.ThreadMessage {
	msgs := f.thread(t, projectID)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Kind == domain.KindApproval {
			return &msgs[i]
		}
	}
	return nil
}

func (f pipelineFixture) hierarchy(t require.TestingT, projectID string) service.SceneView {
	v, err := f.svc.Plans.Hierarchy(context.Background(), projectID, "", "")
	require.NoError(t, err)
	return v
}

func (f pipelineFixture) waitIdle(t *testing.T, projectID string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ok := true
		for _, m := range f.thread(c, projectID) {
			if m.Kind == domain.KindProgress && (m.Job == nil || (m.Job.Status != domain.JobSucceeded && m.Job.Status != domain.JobFailed)) {
				ok = false
			}
		}
		assert.True(c, ok)
	}, 5*time.Second, 10*time.Millisecond, "a plan job never finished")
}

func TestPipeline_RunsProducerWriterThenHoldsAtTheScriptApproval(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	f.completeBrief(t, p.ID)
	f.waitState(t, p.ID, domain.StateAwaitingApproval)
	assert.Equal(t, []llm.Agent{llm.AgentProducer, llm.AgentWriter}, f.llm.agents(), "Producer then Writer, nothing after")

	card := f.approvalCard(t, p.ID)
	require.NotNil(t, card)
	assert.Equal(t, domain.ApprovalPending, card.ApprovalStatus)
	assert.Equal(t, "script_version", card.RefType)
	assert.Contains(t, card.Content, "脚本 v1")
	assert.Contains(t, card.Content, "2 Scene")
	f.waitIdle(t, p.ID)
	assert.Empty(t, f.hierarchy(t, p.ID).Scenes, "no Scene is planned before the script is approved")
	time.Sleep(100 * time.Millisecond)
	assert.Zero(t, f.llm.count(llm.AgentDirector), "the next stage does not start while the card is pending")
	assert.Equal(t, domain.StateAwaitingApproval, f.state(t, p.ID))
}

func TestPipeline_ApprovalRunsDirectorAndStoryboardAndCreatesScenesAndShots(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	f.completeBrief(t, p.ID)
	f.waitState(t, p.ID, domain.StateAwaitingApproval)
	card := f.approvalCard(t, p.ID)
	require.NotNil(t, card)

	_, _, err := f.svc.Chat.RespondApproval(context.Background(), p.ID, card.ID, service.DecisionApprove, "")
	require.NoError(t, err)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Len(c, f.hierarchy(c, p.ID).Scenes, 2) }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, []llm.Agent{llm.AgentProducer, llm.AgentWriter, llm.AgentDirector, llm.AgentStoryboard}, f.llm.agents(),
		"Producer → Writer → Director → Storyboard in this order")
	f.waitState(t, p.ID, domain.StateVisualDesign)

	view := f.hierarchy(t, p.ID)
	assert.Equal(t, "朝", view.Scenes[0].Title)
	assert.Equal(t, 2, view.Scenes[0].ShotCount)
	assert.Equal(t, 1, view.Scenes[1].ShotCount)
	require.NotNil(t, view.Scene)
	require.Len(t, view.Shots, 2)
	assert.Equal(t, "窓の光", view.Shots[0].Objective)
	assert.Equal(t, 5.0, view.Shots[0].Duration)
	assert.Equal(t, domain.ShotPlanning, view.Shots[0].State)
	require.NotNil(t, view.Shots[0].Storyboard)
	assert.Equal(t, "ワイド", view.Shots[0].Storyboard.Framing)
	assert.Equal(t, "寝室", view.Scene.Location, "the Location named by the plan is stored")
	msgs := f.thread(t, p.ID)
	assert.Contains(t, lastMessage(t, msgs).Content, "2 Scene / 3 Shot")
	assert.Empty(t, f.pendingApprovals(t, p.ID))
}

func (f pipelineFixture) pendingApprovals(t *testing.T, projectID string) []domain.ThreadMessage {
	t.Helper()
	var out []domain.ThreadMessage
	for _, m := range f.thread(t, projectID) {
		if m.Kind == domain.KindApproval && m.ApprovalStatus == domain.ApprovalPending {
			out = append(out, m)
		}
	}
	return out
}

func TestPipeline_ApprovingInWordsAlsoContinues(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	f.completeBrief(t, p.ID)
	f.waitState(t, p.ID, domain.StateAwaitingApproval)
	card := f.approvalCard(t, p.ID)
	require.NotNil(t, card)

	_, err := f.svc.Chat.PostMessage(context.Background(), p.ID, service.PostMessageInput{Content: "承認", ReplyToMessageID: card.ID})
	require.NoError(t, err)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Len(c, f.hierarchy(c, p.ID).Scenes, 2) }, 5*time.Second, 10*time.Millisecond)
}

func TestPipeline_ChangeRequestRewritesTheScriptWithTheFeedbackAndAsksAgain(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	f.completeBrief(t, p.ID)
	f.waitState(t, p.ID, domain.StateAwaitingApproval)
	first := f.approvalCard(t, p.ID)
	require.NotNil(t, first)
	_, _, err := f.svc.Chat.RespondApproval(context.Background(), p.ID, first.ID, service.DecisionRequestChanges, "夜のシーンにして")
	require.NoError(t, err)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		card := f.approvalCard(c, p.ID)
		require.NotNil(c, card)
		assert.NotEqual(c, first.ID, card.ID)
		assert.Equal(c, domain.ApprovalPending, card.ApprovalStatus)
	}, 5*time.Second, 10*time.Millisecond, "a second approval card is posted for the rewritten script")
	f.waitState(t, p.ID, domain.StateAwaitingApproval)

	second := f.approvalCard(t, p.ID)
	assert.Contains(t, second.Content, "脚本 v2")
	assert.Equal(t, 2, f.llm.count(llm.AgentProducer))
	assert.Equal(t, 2, f.llm.count(llm.AgentWriter))
	assert.Contains(t, f.llm.lastUser(llm.AgentWriter), "夜のシーンにして", "the user's request reaches the Writer")
	assert.Contains(t, f.llm.lastUser(llm.AgentWriter), "脚本", "the previous script is given to the Writer")
	assert.Zero(t, f.llm.count(llm.AgentDirector), "no planning before the script is approved")
	assert.Empty(t, f.hierarchy(t, p.ID).Scenes)
}

func TestPipeline_FullAutoDoesNotStopForTheScript(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	_, err := f.svc.Projects.SetConfirmationLevel(context.Background(), p.ID, domain.ConfirmFullAuto, nil)
	require.NoError(t, err)
	f.completeBrief(t, p.ID)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Len(c, f.hierarchy(c, p.ID).Scenes, 2) }, 5*time.Second, 10*time.Millisecond)
	assert.Nil(t, f.approvalCard(t, p.ID), "FULL AUTO posts no approval card")
	f.waitState(t, p.ID, domain.StateVisualDesign)
}

func TestPipeline_DoesNotStartWhileTheBriefIsIncomplete(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい") // Objective and Target are missing
	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, f.llm.agents())
	assert.Equal(t, domain.StateDraft, f.state(t, p.ID))
}
