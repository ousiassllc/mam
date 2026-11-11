package budgetstop_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator/genstage"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// The rig is the real Gate over a test database, as in genstage's own tests; the Generation Orchestrator and the chat are fakes that record what they are asked.

const (
	planJSON  = `{"scenes":[{"title":"S1","location":"寝室","shots":[{"objective":"o1","duration_seconds":5},{"objective":"o2","duration_seconds":5},{"objective":"o3","duration_seconds":5}]}]}`
	boardJSON = `{"scenes":[{"shots":[{"description":"f1"},{"description":"f2"},{"description":"f3"}]}]}`
)

type rig struct {
	t        *testing.T
	projects *repository.ProjectRepository
	plans    *repository.PlanRepository
	policies *repository.PolicyRepository
	launches *genapprovalrepo.Repository
	chat     *fakeChat
	gen      *fakeGen
	cfg      modeladapter.Config
	gate     *genstage.Gate
}

func newRig(t *testing.T) *rig {
	t.Helper()
	db := testdb.Open(t)
	r := &rig{t: t, projects: repository.NewProjectRepository(db), plans: repository.NewPlanRepository(db), policies: repository.NewPolicyRepository(db), launches: genapprovalrepo.New(db), cfg: modeladapter.DefaultConfig()}
	r.chat, r.gen = &fakeChat{projects: r.projects}, &fakeGen{errs: map[string]error{}}
	r.gate = genstage.New(genstage.Deps{
		Projects: r.projects, Plans: r.plans, Gens: repository.NewGenerationRepository(db), Policies: r.policies,
		Jobs: repository.NewJobRepository(db), Repo: r.launches, Chat: r.chat,
	})
	r.gate.SetGenerator(r.gen, func() modeladapter.Config { return r.cfg })
	return r
}

// planned creates an AUTO Project in VISUAL_DESIGN whose plan is stored: 1 Scene with 3 Shots, each PLANNING with no Take.
func (r *rig) planned() string {
	r.t.Helper()
	id := domain.NewID()
	require.NoError(r.t, r.projects.Create(context.Background(), repository.NewProject{
		Project: domain.Project{ID: id, State: domain.StateVisualDesign, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmFullAuto,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced},
		Brief:    domain.Brief{ContentType: "商品CM", Objective: "Instagram広告", Subject: "美容液", Target: "20代女性", Duration: "15秒", AspectRatio: "9:16", Delegated: []string{string(domain.BriefMood)}},
		Messages: []domain.ChatMessage{{ProjectID: id, Role: domain.RoleUser, Kind: domain.KindText, Content: "美容液のCMを作りたい"}},
	}))
	var plan domain.ScenePlan
	require.NoError(r.t, json.Unmarshal([]byte(planJSON), &plan))
	var board domain.Storyboard
	require.NoError(r.t, json.Unmarshal([]byte(boardJSON), &board))
	require.NoError(r.t, r.plans.SavePlan(context.Background(), id, plan, board))
	return id
}

// knowPrices prices every image model of the Reference Pack, so that the estimate of the plan is a number.
func (r *rig) knowPrices() {
	for m, price := range map[domain.ModelName]float64{domain.ModelGPTImage26Flare: 0.05, domain.ModelNanoBanana: 0.0336} {
		mc := r.cfg.Models[m]
		mc.CostPerImage = price
		r.cfg.Models[m] = mc
	}
}

// step is one look of the gate at the Project.
func (r *rig) step(id string) {
	r.t.Helper()
	p, err := r.projects.Get(context.Background(), id)
	require.NoError(r.t, err)
	_, err = r.gate.Step(context.Background(), p)
	require.NoError(r.t, err)
}

func (r *rig) shotIDs(projectID string) []string {
	r.t.Helper()
	scenes, err := r.plans.ListScenes(context.Background(), projectID)
	require.NoError(r.t, err)
	var ids []string
	for _, sc := range scenes {
		shots, err := r.plans.ListShots(context.Background(), sc.ID)
		require.NoError(r.t, err)
		for _, sh := range shots {
			ids = append(ids, sh.ID)
		}
	}
	return ids
}

func (r *rig) project1(id string) domain.Project {
	r.t.Helper()
	p, err := r.projects.Get(context.Background(), id)
	require.NoError(r.t, err)
	return p
}

type fakeChat struct {
	projects *repository.ProjectRepository
	mu       sync.Mutex
	notices  []string
	cards    []domain.CardBody
}

func (c *fakeChat) Supersede(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (c *fakeChat) Announce(_ context.Context, _ string, m domain.ChatMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notices = append(c.notices, m.Content)
	return nil
}

// RaiseEscalationCard behaves like service.ChatService: the card holds the Project in AWAITING_APPROVAL.
func (c *fakeChat) RaiseEscalationCard(ctx context.Context, projectID string, body domain.CardBody, _ string) error {
	c.mu.Lock()
	c.cards = append(c.cards, body)
	c.mu.Unlock()
	state := domain.StateAwaitingApproval
	card := domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindEscalation, Content: domain.EncodeCardBody(body)}
	return c.projects.Apply(ctx, repository.Turn{ProjectID: projectID, Messages: []domain.ChatMessage{card}, State: &state})
}

// fakeGen records the starts it is asked for; it makes no job and claims no Shot, so the starts the gate counts are the ones it made.
type fakeGen struct {
	mu    sync.Mutex
	tried []string
	errs  map[string]error // by Shot ID: the start of that Shot fails
}

func (g *fakeGen) start(shotID string) (domain.Job, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tried = append(g.tried, shotID)
	if err := g.errs[shotID]; err != nil {
		return domain.Job{}, err
	}
	return domain.Job{ID: "job-" + shotID}, nil
}

func (g *fakeGen) StartReferencePack(_ context.Context, shotID string) (domain.Job, error) {
	return g.start(shotID)
}

func (g *fakeGen) StartByPolicy(_ context.Context, shotID, _ string, _ []domain.SpecPart) (domain.Job, error) {
	return g.start(shotID)
}

func (g *fakeGen) UsesCandidates(context.Context, domain.Shot) (bool, error) { return false, nil }

func (g *fakeGen) started() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.tried...)
}

func (r *rig) announcement() string {
	r.chat.mu.Lock()
	defer r.chat.mu.Unlock()
	return strings.Join(r.chat.notices, "\n")
}
