package genstage_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator/genstage"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// The test rig runs the real Orchestrator, Gate and repositories over a test database. The LLM, the chat and the Generation Orchestrator are fakes: nothing is generated, and the fake Generation Orchestrator only records what it was asked to start (and makes the job and the Shot claim a real one would).

const (
	conceptJSON = `{"title":"朝の一滴","logline":"肌が目覚める","concept":"朝の光と美容液","tone":"上品","structure":[{"name":"フック","description":"光","duration_seconds":3}]}`
	scriptJSON  = `{"title":"脚本タイトル","scenes":[{"title":"朝","location":"寝室","description":"光が差す"},{"title":"使う","description":"美容液を手に取る"}]}`
	planJSON    = `{"scenes":[{"title":"S1","location":"寝室","shots":[{"objective":"o1","duration_seconds":5},{"objective":"o2","duration_seconds":5},{"objective":"o3","duration_seconds":5}]}]}`
	boardJSON   = `{"scenes":[{"shots":[{"description":"f1"},{"description":"f2"},{"description":"f3"}]}]}`
)

type rig struct {
	t        *testing.T
	db       *repository.DB
	projects *repository.ProjectRepository
	plans    *repository.PlanRepository
	chatRepo *repository.ChatRepository
	gens     *repository.GenerationRepository
	jobs     *repository.JobRepository
	policies *repository.PolicyRepository
	launches *genapprovalrepo.Repository
	chat     *fakeChat
	gen      *fakeGen
	prices   *prices
	planJobs *fakeJobs
	gate     *genstage.Gate
	orch     *orchestrator.Orchestrator
}

func newRig(t *testing.T) *rig {
	t.Helper()
	db := testdb.Open(t)
	r := &rig{
		t: t, db: db, projects: repository.NewProjectRepository(db), plans: repository.NewPlanRepository(db), chatRepo: repository.NewChatRepository(db),
		gens: repository.NewGenerationRepository(db), jobs: repository.NewJobRepository(db), policies: repository.NewPolicyRepository(db),
		launches: genapprovalrepo.New(db), planJobs: &fakeJobs{}, prices: &prices{cfg: modeladapter.DefaultConfig()},
	}
	r.chat = &fakeChat{projects: r.projects}
	r.gen = &fakeGen{gens: r.gens, jobs: r.jobs, errs: map[string]error{}, jobOf: map[string]string{}}
	r.restart()
	return r
}

// restart builds a new Gate and Orchestrator over the same database: the state of a process that was started again.
func (r *rig) restart() {
	r.gate = genstage.New(genstage.Deps{Projects: r.projects, Plans: r.plans, Gens: r.gens, Policies: r.policies, Jobs: r.jobs, Repo: r.launches, Chat: r.chat})
	r.gate.SetGenerator(r.gen, r.prices.get)
	r.orch = orchestrator.New(orchestrator.Deps{
		Projects: r.projects, Plans: r.plans, ChatRepo: r.chatRepo, Chat: r.chat, Jobs: r.planJobs, LLM: fakeLLM{}, Gate: r.gate,
	})
}

func completeBrief() domain.Brief {
	return domain.Brief{
		ContentType: "商品CM", Objective: "Instagram広告", Subject: "美容液", Target: "20代女性",
		Duration: "15秒", AspectRatio: "9:16", Delegated: []string{string(domain.BriefMood)},
	}
}

// project creates a Project; state "" is DRAFT with a complete Brief.
func (r *rig) project(state domain.ProjectState, mode domain.Mode, level domain.ConfirmationLevel) string {
	r.t.Helper()
	if state == "" {
		state = domain.StateDraft
	}
	id := domain.NewID()
	require.NoError(r.t, r.projects.Create(context.Background(), repository.NewProject{
		Project: domain.Project{
			ID: id, State: state, Mode: mode, ConfirmationLevel: level,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced,
		},
		Brief:    completeBrief(),
		Messages: []domain.ChatMessage{{ProjectID: id, Role: domain.RoleUser, Kind: domain.KindText, Content: "美容液のCMを作りたい"}},
	}))
	return id
}

// planned creates an AUTO Project in VISUAL_DESIGN whose Scene / Shot plan is stored: 1 Scene with 3 Shots, each PLANNING with no Take.
func (r *rig) planned(level domain.ConfirmationLevel) string {
	r.t.Helper()
	id := r.project(domain.StateVisualDesign, domain.ModeAuto, level)
	var plan domain.ScenePlan
	require.NoError(r.t, json.Unmarshal([]byte(planJSON), &plan))
	var board domain.Storyboard
	require.NoError(r.t, json.Unmarshal([]byte(boardJSON), &board))
	require.NoError(r.t, r.plans.SavePlan(context.Background(), id, plan, board))
	return id
}

func (r *rig) handle(projectID string) {
	r.t.Helper()
	payload, _ := json.Marshal(map[string]string{"project_id": projectID})
	require.NoError(r.t, r.orch.Handle(context.Background(), domain.Job{ID: "job-x", Kind: domain.JobPlan, Payload: payload}))
}

func (r *rig) project1(id string) domain.Project {
	r.t.Helper()
	p, err := r.projects.Get(context.Background(), id)
	require.NoError(r.t, err)
	return p
}

func (r *rig) shotIDs(projectID string) []string {
	r.t.Helper()
	ctx := context.Background()
	scenes, err := r.plans.ListScenes(ctx, projectID)
	require.NoError(r.t, err)
	var ids []string
	for _, sc := range scenes {
		shots, err := r.plans.ListShots(ctx, sc.ID)
		require.NoError(r.t, err)
		for _, sh := range shots {
			ids = append(ids, sh.ID)
		}
	}
	return ids
}

// legacyCard posts a generation approval card of v0.1.34 (#1108) the way that version did: a card with its record (the scope it showed), and the Project holds in AWAITING_APPROVAL while it is pending. No code of this version posts one.
func (r *rig) legacyCard(projectID string, status domain.ApprovalStatus) string {
	r.t.Helper()
	return r.legacyCardWithID(projectID, status, "")
}

// legacyCardWithID is legacyCard with the message ID given ("" is a new one): the card of an imported bundle keeps the ID it was made with.
func (r *rig) legacyCardWithID(projectID string, status domain.ApprovalStatus, id string) string {
	r.t.Helper()
	ctx := context.Background()
	plan, err := r.gate.Assess(ctx, domain.Project{ID: projectID, Mode: domain.ModeAuto, State: domain.StateVisualDesign}) // what the card showed
	require.NoError(r.t, err)
	state := domain.StateAwaitingApproval
	card := domain.ChatMessage{
		ID: id, Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: "生成の承認: 旧方針のカード",
		RefType: domain.CheckpointGeneration.RefType(), RefID: "fingerprint", ApprovalStatus: domain.ApprovalPending,
	}
	require.NoError(r.t, r.projects.Apply(ctx, repository.Turn{ProjectID: projectID, Messages: []domain.ChatMessage{card}, State: &state}))
	c := r.card(projectID)
	require.NotNil(r.t, c)
	require.NoError(r.t, r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		scope, _ := json.Marshal(plan.Plan.Lines)
		_, err := tx.ExecContext(ctx, `INSERT INTO generation_approval (message_id, project_id, fingerprint, scope) VALUES (?, ?, 'fingerprint', ?)`, c.ID, projectID, string(scope))
		return err
	}))
	if status != domain.ApprovalPending {
		r.answer(projectID, status)
	}
	return c.ID
}

// withoutRecord deletes the record of the card, which a bundle does not carry (generation_approval is not bundled): the card is left as an imported one.
func (r *rig) withoutRecord(cardID string) {
	r.t.Helper()
	require.NoError(r.t, r.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM generation_approval WHERE message_id = ?`, cardID)
		return err
	}))
}

// card is the newest generation approval card of the Project (nil when none).
func (r *rig) card(projectID string) *genapprovalrepo.Card {
	r.t.Helper()
	c, err := r.launches.LatestCard(context.Background(), projectID)
	require.NoError(r.t, err)
	return c
}

// cards counts the generation approval cards of the Project: this version posts none.
func (r *rig) cards(projectID string) int {
	r.t.Helper()
	var n int
	require.NoError(r.t, r.db.Reader().QueryRow(`SELECT COUNT(*) FROM chat_message WHERE project_id = ? AND kind = 'approval' AND ref_type = ?`,
		projectID, domain.CheckpointGeneration.RefType()).Scan(&n))
	return n
}

// answer records the user's decision on the newest generation card and moves the Project out of AWAITING_APPROVAL as the chat service does.
func (r *rig) answer(projectID string, status domain.ApprovalStatus) {
	r.t.Helper()
	c := r.card(projectID)
	require.NotNil(r.t, c)
	resume := domain.CheckpointGeneration.ApprovedState()
	if status == domain.ApprovalChangesRequested {
		resume = domain.CheckpointGeneration.ChangesRequestedState()
	}
	user := domain.ChatMessage{Role: domain.RoleUser, Kind: domain.KindText, Content: "返信", ReplyToMessageID: c.ID}
	require.NoError(r.t, r.projects.Apply(context.Background(), repository.Turn{
		ProjectID: projectID, Approval: &repository.ApprovalUpdate{MessageID: c.ID, Status: status}, ResumeState: &resume,
		Messages: []domain.ChatMessage{user},
	}))
}

// approveCard answers a card of another checkpoint (the script) as the chat service does, resuming the Project in resume.
func (r *rig) approveCard(projectID, cardID string, resume domain.ProjectState) {
	r.t.Helper()
	require.NoError(r.t, r.projects.Apply(context.Background(), repository.Turn{
		ProjectID: projectID, Approval: &repository.ApprovalUpdate{MessageID: cardID, Status: domain.ApprovalApproved}, ResumeState: &resume,
	}))
}

// setMode switches the mode of the Project.
func (r *rig) setMode(projectID string, mode domain.Mode) {
	r.t.Helper()
	require.NoError(r.t, r.projects.Apply(context.Background(), repository.Turn{ProjectID: projectID, Mode: &mode}))
}

// userWrites is a message of the user in the thread that does not answer a card.
func (r *rig) userWrites(projectID, text string) {
	r.t.Helper()
	require.NoError(r.t, r.projects.Apply(context.Background(), repository.Turn{ProjectID: projectID,
		Messages: []domain.ChatMessage{{Role: domain.RoleUser, Kind: domain.KindText, Content: text}}}))
}

// announcements counts the announcements of the automatic start in the thread.
func (r *rig) announcements() (n int) {
	for _, text := range r.chat.notices() {
		if strings.Contains(text, "承認を待たずに生成を始めます") {
			n++
		}
	}
	return n
}

// announcement is the text of the (first) announcement of the automatic start.
func (r *rig) announcement() string {
	for _, text := range r.chat.notices() {
		if strings.Contains(text, "承認を待たずに生成を始めます") {
			return text
		}
	}
	return ""
}

// endJob ends the latest job of the Shot the way the generation does: the Shot is released, the job succeeded or failed.
func (r *rig) endJob(shotID string, failed bool) {
	r.t.Helper()
	ctx := context.Background()
	jobID := r.gen.jobOf[shotID]
	require.NotEmpty(r.t, jobID)
	require.NoError(r.t, r.gens.SetShotState(ctx, shotID, domain.ShotPlanning))
	require.NoError(r.t, r.jobs.MarkRunning(ctx, jobID))
	if failed {
		require.NoError(r.t, r.jobs.MarkFailed(ctx, jobID, "boom"))
		return
	}
	require.NoError(r.t, r.jobs.MarkSucceeded(ctx, jobID))
}

var errBoom = errors.New("boom")
