package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

const (
	conceptJSON = `{"title":"朝の一滴","logline":"肌が目覚める","concept":"朝の光と美容液","tone":"上品","structure":[{"name":"フック","description":"光","duration_seconds":3}]}`
	scriptJSON  = `{"title":"脚本タイトル","scenes":[{"title":"朝","location":"寝室","description":"光が差す"},{"title":"使う","description":"美容液を手に取る"}]}`
	planJSON    = `{"scenes":[{"title":"S1","location":"寝室","shots":[{"objective":"o1","duration_seconds":5},{"objective":"o2","duration_seconds":5},{"objective":"o3","duration_seconds":5}]}]}`
	boardJSON   = `{"scenes":[{"shots":[{"description":"f1"},{"description":"f2"},{"description":"f3"}]}]}`
)

// fakeLLM stands in for the Reasoning LLM Adapter: each Agent answers with a canned text, or with
// the error configured for it. after runs once the call is recorded and may replace the result.
type fakeLLM struct {
	mu      sync.Mutex
	answers map[llm.Agent]string
	errs    map[llm.Agent]error
	calls   []llm.Agent
	users   map[llm.Agent][]string
	onCall  func(agent llm.Agent) error
}

func newFakeLLM() *fakeLLM {
	return &fakeLLM{
		answers: map[llm.Agent]string{
			llm.AgentProducer: conceptJSON, llm.AgentWriter: scriptJSON,
			llm.AgentDirector: planJSON, llm.AgentStoryboard: boardJSON,
		},
		errs:  map[llm.Agent]error{},
		users: map[llm.Agent][]string{},
	}
}

func (f *fakeLLM) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, agent)
	f.users[agent] = append(f.users[agent], req.User)
	hook, err := f.onCall, f.errs[agent]
	text := f.answers[agent]
	f.mu.Unlock()
	if hook != nil {
		if herr := hook(agent); herr != nil {
			return llm.Response{}, herr
		}
	}
	if err != nil {
		return llm.Response{}, err
	}
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

func (f *fakeLLM) called() []llm.Agent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]llm.Agent(nil), f.calls...)
}

func (f *fakeLLM) promptsOf(agent llm.Agent) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.users[agent]...)
}

// fakeChat behaves like service.ChatService for the two calls the Orchestrator makes: it holds the
// Project in AWAITING_APPROVAL with a pending card when the Confirmation Level asks for one.
type fakeChat struct {
	projects *repository.ProjectRepository

	mu         sync.Mutex
	announced  []domain.ChatMessage
	approvals  int
	announceFn func(m domain.ChatMessage) error
	approvalFn func() error
}

func (c *fakeChat) RequestApproval(ctx context.Context, projectID string, cp domain.Checkpoint, summary, refID string) (bool, error) {
	c.mu.Lock()
	c.approvals++
	fn := c.approvalFn
	c.mu.Unlock()
	if fn != nil {
		if err := fn(); err != nil {
			return false, err
		}
	}
	p, err := c.projects.Get(ctx, projectID)
	if err != nil {
		return false, err
	}
	if !p.ConfirmationLevel.RequiresApproval(cp, p.CustomCheckpoints) {
		return false, nil
	}
	state := domain.StateAwaitingApproval
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: summary,
		RefType: cp.RefType(), RefID: refID, ApprovalStatus: domain.ApprovalPending,
	}
	return true, c.projects.Apply(ctx, repository.Turn{ProjectID: projectID, Messages: []domain.ChatMessage{card}, State: &state})
}

func (c *fakeChat) Announce(_ context.Context, _ string, m domain.ChatMessage) error {
	c.mu.Lock()
	c.announced = append(c.announced, m)
	fn := c.announceFn
	c.mu.Unlock()
	if fn != nil {
		return fn(m)
	}
	return nil
}

func (c *fakeChat) texts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, m := range c.announced {
		out = append(out, m.Content)
	}
	return out
}

func (c *fakeChat) kinds() []domain.MessageKind {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []domain.MessageKind
	for _, m := range c.announced {
		out = append(out, m.Kind)
	}
	return out
}

type fakeJobs struct {
	mu       sync.Mutex
	payloads []any
	err      error
}

func (j *fakeJobs) Enqueue(_ context.Context, kind domain.JobKind, payload any) (domain.Job, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.err != nil {
		return domain.Job{}, j.err
	}
	j.payloads = append(j.payloads, payload)
	return domain.Job{ID: fmt.Sprintf("job-%d", len(j.payloads)), Kind: kind}, nil
}

func (j *fakeJobs) count() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.payloads)
}

type fixture struct {
	db       *repository.DB
	projects *repository.ProjectRepository
	plans    *repository.PlanRepository
	chatRepo *repository.ChatRepository
	notes    *repository.NoteRepository
	llm      *fakeLLM
	chat     *fakeChat
	jobs     *fakeJobs
	orch     *orchestrator.Orchestrator
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	f := &fixture{
		db: db, projects: repository.NewProjectRepository(db), plans: repository.NewPlanRepository(db),
		chatRepo: repository.NewChatRepository(db), notes: repository.NewNoteRepository(db),
		llm: newFakeLLM(), jobs: &fakeJobs{},
	}
	f.chat = &fakeChat{projects: f.projects}
	f.orch = orchestrator.New(orchestrator.Deps{
		Projects: f.projects, Plans: f.plans, ChatRepo: f.chatRepo, Chat: f.chat, Jobs: f.jobs, LLM: f.llm, Notes: f.notes,
	})
	return f
}

func completeBrief() domain.Brief {
	return domain.Brief{
		ContentType: "商品CM", Objective: "Instagram広告", Subject: "美容液", Target: "20代女性",
		Duration: "15秒", AspectRatio: "9:16", Delegated: []string{string(domain.BriefMood)},
	}
}

type seed struct {
	state domain.ProjectState
	mode  domain.Mode
	level domain.ConfirmationLevel
	brief domain.Brief
}

func (f *fixture) project(t *testing.T, s seed) string {
	t.Helper()
	if s.state == "" {
		s.state = domain.StateDraft
	}
	if s.mode == "" {
		s.mode = domain.ModeAuto
	}
	if s.level == "" {
		s.level = domain.ConfirmFullAuto
	}
	id := domain.NewID()
	require.NoError(t, f.projects.Create(context.Background(), repository.NewProject{
		Project: domain.Project{
			ID: id, State: s.state, Mode: s.mode, ConfirmationLevel: s.level,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced,
		},
		Brief:    s.brief,
		Messages: []domain.ChatMessage{{ProjectID: id, Role: domain.RoleUser, Kind: domain.KindText, Content: "美容液のCMを作りたい"}},
	}))
	return id
}

func (f *fixture) handle(ctx context.Context, projectID string) error {
	payload, _ := json.Marshal(map[string]string{"project_id": projectID})
	return f.orch.Handle(ctx, domain.Job{ID: "job-x", Kind: domain.JobPlan, Payload: payload})
}

func (f *fixture) state(t *testing.T, projectID string) domain.ProjectState {
	t.Helper()
	p, err := f.projects.Get(context.Background(), projectID)
	require.NoError(t, err)
	return p.State
}

func (f *fixture) saveScript(t *testing.T, projectID string) domain.ScriptVersion {
	t.Helper()
	var doc domain.ScriptDoc
	require.NoError(t, json.Unmarshal([]byte(`{"Concept":`+conceptJSON+`,"Script":`+scriptJSON+`}`), &doc))
	v, err := f.plans.SaveScript(context.Background(), projectID, doc)
	require.NoError(t, err)
	return v
}

func (f *fixture) scriptCard(t *testing.T, projectID, scriptID string) *domain.ChatMessage {
	t.Helper()
	card, err := f.chatRepo.LatestApprovalCard(context.Background(), projectID, domain.CheckpointScript.RefType(), scriptID)
	require.NoError(t, err)
	return card
}

// answer records the user's decision on an Approval card and resumes the Project like the chat service does.
func (f *fixture) answer(t *testing.T, projectID string, card *domain.ChatMessage, status domain.ApprovalStatus, reply string) {
	t.Helper()
	resume := domain.StatePlanning
	turn := repository.Turn{
		ProjectID:   projectID,
		Approval:    &repository.ApprovalUpdate{MessageID: card.ID, Status: status},
		ResumeState: &resume,
	}
	if reply != "" {
		turn.Messages = []domain.ChatMessage{{Role: domain.RoleUser, Kind: domain.KindText, Content: reply, ReplyToMessageID: card.ID}}
	}
	require.NoError(t, f.projects.Apply(context.Background(), turn))
}

func (f *fixture) sceneCount(t *testing.T, projectID string) int {
	t.Helper()
	scenes, err := f.plans.ListScenes(context.Background(), projectID)
	require.NoError(t, err)
	return len(scenes)
}

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
		{"DRAFT with a missing Brief item", seed{brief: incomplete}, false},
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

func TestKick_AFailingProgressCardDoesNotFailTheKick(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	f.chat.announceFn = func(domain.ChatMessage) error { return errors.New("chat is down") }

	require.NoError(t, f.orch.Kick(context.Background(), id))
	assert.Equal(t, 1, f.jobs.count())
}

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

func TestHandle_DraftWithAMissingBriefItemDoesNotStart(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: domain.Brief{Subject: "美容液"}})

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
			wantText: "AI サービスの呼び出しに失敗しました",
			wantLogic: func(t *testing.T, f *fixture, id string) {
				_, err := f.plans.LatestScript(context.Background(), id)
				assert.ErrorIs(t, err, repository.ErrNotFound, "no script is stored")
			},
		},
		{
			name: "Writer fails with a rate limit", agent: llm.AgentWriter, err: apiErr(429),
			wantState: domain.StatePlanning, wantCalls: []llm.Agent{llm.AgentProducer, llm.AgentWriter},
			wantText: "AI サービスのレート制限に達しました",
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
			wantText: "タイムアウト",
		},
		{
			name: "Director fails", agent: llm.AgentDirector, err: apiErr(503),
			wantState: domain.StateVisualDesign, wantCalls: []llm.Agent{llm.AgentProducer, llm.AgentWriter, llm.AgentDirector},
			wantText: "AI サービスの呼び出しに失敗しました",
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
			wantText: "内部エラーが発生しました",
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
			assert.Contains(t, err.Error(), "AI の出力を制作に使える形で受け取れませんでした")
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
	assert.Contains(t, err.Error(), "タイムアウト")
}

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
	assert.Contains(t, err.Error(), "内部エラーが発生しました")
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
