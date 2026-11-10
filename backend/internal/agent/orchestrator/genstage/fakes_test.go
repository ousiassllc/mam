package genstage_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

type fakeLLM struct{}

func (fakeLLM) Complete(_ context.Context, agent llm.Agent, _ llm.Request) (llm.Response, error) {
	text := map[llm.Agent]string{llm.AgentProducer: conceptJSON, llm.AgentWriter: scriptJSON, llm.AgentDirector: planJSON, llm.AgentStoryboard: boardJSON}[agent]
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

// fakeChat behaves like service.ChatService for the calls the Gate and the Orchestrator make.
type fakeChat struct {
	projects *repository.ProjectRepository

	mu          sync.Mutex
	announced   []domain.ChatMessage
	checkpoints []domain.Checkpoint // the checkpoints an Approval card was requested at
	escalations []string
	superseded  []string // the IDs of the legacy cards the system closed
}

func (c *fakeChat) RequestApproval(ctx context.Context, projectID string, cp domain.Checkpoint, summary, refID string) (bool, error) {
	p, err := c.projects.Get(ctx, projectID)
	if err != nil {
		return false, err
	}
	if !p.ConfirmationLevel.RequiresApproval(cp, p.CustomCheckpoints) {
		return false, nil
	}
	c.mu.Lock()
	c.checkpoints = append(c.checkpoints, cp)
	c.mu.Unlock()
	state := domain.StateAwaitingApproval
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: summary,
		RefType: cp.RefType(), RefID: refID, ApprovalStatus: domain.ApprovalPending,
	}
	return true, c.projects.Apply(ctx, repository.Turn{ProjectID: projectID, Messages: []domain.ChatMessage{card}, State: &state})
}

// Supersede behaves like service.ChatService.Supersede: the card is closed as approved by a message of the assistant (no message of the user), and the Project returns from AWAITING_APPROVAL.
func (c *fakeChat) Supersede(ctx context.Context, projectID, cardID, notice string) (bool, error) {
	resume := domain.CheckpointGeneration.ApprovedState()
	err := c.projects.Apply(ctx, repository.Turn{
		ProjectID: projectID, Approval: &repository.ApprovalUpdate{MessageID: cardID, Status: domain.ApprovalApproved}, ResumeState: &resume,
		Messages: []domain.ChatMessage{{Role: domain.RoleAssistant, Kind: domain.KindText, Content: notice}},
	})
	if errors.Is(err, repository.ErrConflict) {
		return false, nil
	}
	if err == nil {
		c.mu.Lock()
		c.superseded = append(c.superseded, cardID)
		c.mu.Unlock()
	}
	return err == nil, err
}

func (c *fakeChat) Announce(_ context.Context, _ string, m domain.ChatMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.announced = append(c.announced, m)
	return nil
}

func (c *fakeChat) RaiseEscalationCard(ctx context.Context, projectID string, body domain.CardBody, _ string) error {
	c.mu.Lock()
	c.escalations = append(c.escalations, body.Text)
	c.mu.Unlock()
	state := domain.StateAwaitingApproval
	card := domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindEscalation, Content: domain.EncodeCardBody(body)}
	return c.projects.Apply(ctx, repository.Turn{ProjectID: projectID, Messages: []domain.ChatMessage{card}, State: &state})
}

func (c *fakeChat) notices() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, m := range c.announced {
		out = append(out, m.Content)
	}
	return out
}

// call is a start the Gate asked the Generation Orchestrator for.
type call struct {
	stage  genapprovalrepo.Stage
	shotID string
}

// fakeGen stands in for generation.Orchestrator: a start claims the Shot and queues a real job row (so that its status can be set), as the real one does, and is recorded. errs make a start of a Shot fail.
type fakeGen struct {
	gens *repository.GenerationRepository
	jobs *repository.JobRepository

	mu         sync.Mutex
	calls      []call
	tried      []string         // the Shots a start was asked for, whatever came of it
	errs       map[string]error // by Shot ID
	cancelOn   string           // the Shot whose start cancels the context of the Gate (the user cancelled the job)
	cancel     context.CancelFunc
	candidates bool
	jobOf      map[string]string // Shot ID -> its latest job
}

func (g *fakeGen) StartReferencePack(ctx context.Context, shotID string) (domain.Job, error) {
	return g.start(ctx, genapprovalrepo.StageReferencePack, shotID)
}

func (g *fakeGen) StartByPolicy(ctx context.Context, shotID, _ string, _ []domain.SpecPart) (domain.Job, error) {
	return g.start(ctx, genapprovalrepo.StageGenerate, shotID)
}

func (g *fakeGen) UsesCandidates(context.Context, domain.Shot) (bool, error) {
	return g.candidates, nil
}

func (g *fakeGen) start(ctx context.Context, stage genapprovalrepo.Stage, shotID string) (domain.Job, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tried = append(g.tried, shotID)
	if err := g.errs[shotID]; err != nil {
		return domain.Job{}, err
	}
	if shotID == g.cancelOn && g.cancel != nil {
		g.cancel()
		return domain.Job{}, ctx.Err()
	}
	claimed, err := g.gens.StartShot(ctx, shotID)
	if err != nil {
		return domain.Job{}, err
	}
	if !claimed {
		return domain.Job{}, generation.ErrShotBusy
	}
	payload, _ := json.Marshal(map[string]string{"shot_id": shotID, "action": string(stage)})
	job, err := g.jobs.Create(ctx, domain.JobGenerate, payload)
	if err != nil {
		return domain.Job{}, err
	}
	g.calls = append(g.calls, call{stage, shotID})
	g.jobOf[shotID] = job.ID
	return job, nil
}

func (g *fakeGen) started() []call {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]call(nil), g.calls...)
}

func (g *fakeGen) count(stage genapprovalrepo.Stage) (n int) {
	for _, c := range g.started() {
		if c.stage == stage {
			n++
		}
	}
	return n
}

type fakeJobs struct {
	mu sync.Mutex
	n  int
}

func (j *fakeJobs) Enqueue(context.Context, domain.JobKind, any) (domain.Job, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.n++
	return domain.Job{ID: "plan-job", Kind: domain.JobPlan}, nil
}

func (j *fakeJobs) count() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.n
}

// prices is the Model Adapter setting the Gate reads; a test changes it between two looks.
type prices struct {
	mu  sync.Mutex
	cfg modeladapter.Config
}

func (p *prices) get() modeladapter.Config {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cfg
}

func (p *prices) edit(fn func(modeladapter.Config)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(p.cfg)
}

// price sets the price of one image of a model.
func (p *prices) price(m domain.ModelName, perImage float64) {
	p.edit(func(c modeladapter.Config) {
		mc := c.Models[m]
		mc.CostPerImage = perImage
		c.Models[m] = mc
	})
}

// pricesKnown prices every image model of the Reference Pack, so that the estimate of the plan is a number.
func (p *prices) pricesKnown() {
	p.price(domain.ModelGPTImage26Flare, 0.05)
	p.price(domain.ModelNanoBanana, 0.0336)
}

func (g *fakeGen) attempts() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.tried...)
}
