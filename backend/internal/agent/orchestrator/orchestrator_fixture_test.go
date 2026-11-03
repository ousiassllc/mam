package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
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
	// onEnqueue, when set, runs first in every Enqueue (without the lock) and may fail it.
	onEnqueue func() error
}

func (j *fakeJobs) Enqueue(_ context.Context, kind domain.JobKind, payload any) (domain.Job, error) {
	if j.onEnqueue != nil {
		if err := j.onEnqueue(); err != nil {
			return domain.Job{}, err
		}
	}
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
	db := testdb.Open(t)

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
