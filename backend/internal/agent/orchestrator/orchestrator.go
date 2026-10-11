// Package orchestrator is the Orchestrator Agent: it drives the Core State Machine of a Project
// (spec/spec.md §22) and decides which Agent runs next (§20.1). This package runs the planning
// part of the AUTO pipeline (spec/spec.md §4.3):
//
//	DRAFT ──Brief complete──▶ PLANNING ──Producer → Writer──▶ script_version
//	        ──(script approval card, when the Confirmation Level asks)──▶ VISUAL_DESIGN
//	        ──Director → Storyboard──▶ scene / shot
//
// Every move is persisted (repository.ProjectRepository.Transition) before the next Agent runs,
// and every step derives what is left to do from what is stored, so an interrupted run is
// resumed by running the same job again.
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/keylock"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Chat is what the Orchestrator needs from the chat thread. *service.ChatService implements it.
type Chat interface {
	// RequestApproval posts the Approval card of a checkpoint when the Confirmation Level asks
	// for one and holds the Project in AWAITING_APPROVAL. It reports whether a card was posted.
	RequestApproval(ctx context.Context, projectID string, cp domain.Checkpoint, summary, refID string) (bool, error)
	// Announce posts an AI message that does not block the Project (text or progress card).
	Announce(ctx context.Context, projectID string, m domain.ChatMessage) error
}

// Jobs is the background job queue. *jobqueue.Queue implements it.
type Jobs interface {
	Enqueue(ctx context.Context, kind domain.JobKind, payload any) (domain.Job, error)
}

// Deps are the collaborators of an Orchestrator.
type Deps struct {
	Projects *repository.ProjectRepository
	Plans    *repository.PlanRepository
	ChatRepo *repository.ChatRepository
	Chat     Chat
	Jobs     Jobs
	// LLM is the Reasoning LLM Adapter every planning Agent reasons through.
	LLM llm.Completer
	// Notes are the production notes of the Project; the Writer and Director are given the relevant
	// ones (spec/spec.md §9.4). Nil: no notes.
	Notes  *repository.NoteRepository
	Logger *slog.Logger
}

// Orchestrator drives the planning pipeline. It is safe for concurrent use.
type Orchestrator struct {
	projects *repository.ProjectRepository
	plans    *repository.PlanRepository
	chatRepo *repository.ChatRepository
	notes    *repository.NoteRepository
	chat     Chat
	jobs     Jobs
	logger   *slog.Logger

	producer     *planning.Producer
	writer       *planning.Writer
	director     *planning.Director
	storyboarder *planning.Storyboarder

	mu     sync.Mutex
	active map[string]bool // a plan job of the Project is queued or running
	rerun  map[string]bool // something happened while that job was running
	locks  keylock.Locker  // one advance at a time per Project
}

// New returns an Orchestrator.
func New(d Deps) *Orchestrator {
	logger := d.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Orchestrator{
		projects: d.Projects, plans: d.Plans, chatRepo: d.ChatRepo, notes: d.Notes, chat: d.Chat, jobs: d.Jobs, logger: logger,
		producer:     planning.NewProducer(d.LLM),
		writer:       planning.NewWriter(d.LLM),
		director:     planning.NewDirector(d.LLM),
		storyboarder: planning.NewStoryboarder(d.LLM),
		active:       make(map[string]bool),
		rerun:        make(map[string]bool),
	}
}

// planPayload is the payload of a JobPlan job.
type planPayload struct {
	ProjectID string `json:"project_id"`
}

// progressText is the content of the progress card of a plan job.
const progressText = "企画・脚本・Scene/Shot 計画を作成しています"

// Kick makes sure the Project is moving: it starts a plan job when the Project is in AUTO mode,
// waits in a state the planning pipeline drives (DRAFT with a complete Brief, PLANNING,
// VISUAL_DESIGN without Scenes) and no job is on it yet. When a job is already on the Project,
// the job is told to look at the Project once more before it ends, so a decision the user made
// meanwhile (an approval) is never missed. Callers invoke it after every change a user makes.
func (o *Orchestrator) Kick(ctx context.Context, projectID string) error {
	p, err := o.projects.Get(ctx, projectID)
	if err != nil {
		return err
	}
	ready, err := o.needsWork(ctx, p)
	if err != nil || !ready {
		return err
	}

	o.mu.Lock()
	if o.active[projectID] {
		o.rerun[projectID] = true
		o.mu.Unlock()
		return nil
	}
	o.active[projectID] = true
	o.mu.Unlock()

	job, err := o.jobs.Enqueue(ctx, domain.JobPlan, planPayload{ProjectID: projectID})
	if err != nil {
		o.release(projectID)
		return fmt.Errorf("enqueue plan job: %w", err)
	}
	card := domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindProgress, Content: progressText, RefID: job.ID}
	if err := o.chat.Announce(ctx, projectID, card); err != nil {
		o.logger.Error("post progress card failed", "project_id", projectID, "job_id", job.ID, "err", err)
	}
	return nil
}

// needsWork reports whether the pipeline has anything to do for the Project right now.
func (o *Orchestrator) needsWork(ctx context.Context, p domain.Project) (bool, error) {
	if p.Mode != domain.ModeAuto {
		return false, nil
	}
	switch p.State {
	case domain.StateDraft:
		brief, err := o.projects.GetBrief(ctx, p.ID)
		if err != nil {
			return false, err
		}
		return len(brief.Missing()) == 0, nil
	case domain.StatePlanning:
		return true, nil
	case domain.StateVisualDesign:
		has, err := o.plans.HasScenes(ctx, p.ID)
		return !has, err
	}
	return false, nil
}

// finish ends the job's claim on the Project. It reports whether a decision came in meanwhile,
// in which case the claim is kept and the caller MUST advance the Project again.
func (o *Orchestrator) finish(projectID string) (again bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.rerun[projectID] {
		delete(o.rerun, projectID)
		return true
	}
	delete(o.active, projectID)
	return false
}

// release drops the claim of a Kick whose job was never queued. A rerun a concurrent Kick asked
// for is dropped too: no job exists to serve it, and keeping the claim would turn every later
// Kick into a rerun request nobody answers. The Project state is persisted, so the next Kick
// sees the same work.
func (o *Orchestrator) release(projectID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.rerun, projectID)
	delete(o.active, projectID)
}

// Handle is the job handler of domain.JobPlan (register it as resumable: every step is
// idempotent). When a Reasoning LLM call fails for good, it tells the user in the chat and
// returns the failure, so the job is recorded as failed; the next message of the user starts
// a new run from where this one stopped.
func (o *Orchestrator) Handle(ctx context.Context, job domain.Job) error {
	var payload planPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil || payload.ProjectID == "" {
		return fmt.Errorf("plan job %s: invalid payload", job.ID)
	}
	id := payload.ProjectID
	unlock, err := o.locks.Lock(ctx, id)
	if err != nil {
		return err // shutting down while the job before this one is still running: the job is queued again
	}
	defer unlock()

	// A resumed job (after a restart) has no claim yet: take it, so that Kick does not start a second job.
	o.mu.Lock()
	o.active[id] = true
	o.mu.Unlock()
	claimed := true
	defer func() {
		if claimed { // an error or a panic: release the claim so the next Kick can start a new job
			o.mu.Lock()
			delete(o.rerun, id)
			delete(o.active, id)
			o.mu.Unlock()
		}
	}()

	for {
		if err := o.advance(ctx, id); err != nil {
			return o.report(ctx, id, err)
		}
		if !o.finish(id) {
			claimed = false
			return nil
		}
	}
}

// report tells the user that the pipeline stopped and returns the error for the job record.
func (o *Orchestrator) report(ctx context.Context, projectID string, err error) error {
	if ctx.Err() != nil {
		return err // shutting down: the job is queued again, nobody is told
	}
	if errors.Is(err, repository.ErrNotFound) {
		return err // the Project is gone
	}
	o.logger.Error("planning pipeline failed", "project_id", projectID, "err", err)
	reason := failureReason(err)
	notice := assistantText("制作を続けられませんでした: " + reason + "\n対処したら、何かメッセージを送ると続きから再開します。")
	if aerr := o.chat.Announce(context.WithoutCancel(ctx), projectID, notice); aerr != nil {
		o.logger.Error("post failure notice failed", "project_id", projectID, "err", aerr)
	}
	return errors.New(reason)
}

// failureReason is the user-facing cause of a failure. Details stay in the log.
func failureReason(err error) string {
	var api *llm.APIError
	switch {
	case errors.Is(err, llm.ErrNoAPIKey):
		return "LLM の API キーが設定されていません（設定画面で Anthropic / OpenAI / Google のキーを登録してください）"
	case errors.Is(err, planning.ErrInvalidPlan), errors.Is(err, llm.ErrInvalidOutput):
		return "AI の出力を制作に使える形で受け取れませんでした"
	case errors.Is(err, llm.ErrTimeout):
		return "AI の応答がタイムアウトしました"
	case errors.Is(err, llm.ErrCircuitOpen):
		return "AI サービスが一時的に利用できません。しばらく待ってから再実行してください"
	case errors.As(err, &api) && api.Status == 429:
		return "AI サービスのレート制限に達しました"
	case errors.As(err, &api):
		return "AI サービスの呼び出しに失敗しました"
	}
	return "内部エラーが発生しました"
}

func assistantText(content string) domain.ChatMessage {
	return domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: content}
}

// advance runs the pipeline from the Project's current state until it has to wait for the
// user (an Approval card), has nothing more to do, or fails.
func (o *Orchestrator) advance(ctx context.Context, projectID string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p, err := o.projects.Get(ctx, projectID)
		if err != nil {
			return err
		}
		if p.Mode != domain.ModeAuto {
			return nil // DIRECTOR MODE: the user drives
		}
		var progressed bool
		switch p.State {
		case domain.StateDraft:
			progressed, err = o.startPlanning(ctx, p)
		case domain.StatePlanning:
			progressed, err = o.scriptStage(ctx, p)
		case domain.StateVisualDesign:
			progressed, err = o.shotPlanStage(ctx, p)
		}
		if err != nil || !progressed {
			return err
		}
	}
}

// startPlanning moves a DRAFT Project with a complete Brief to PLANNING.
func (o *Orchestrator) startPlanning(ctx context.Context, p domain.Project) (bool, error) {
	brief, err := o.projects.GetBrief(ctx, p.ID)
	if err != nil {
		return false, err
	}
	if len(brief.Missing()) > 0 {
		return false, nil
	}
	return true, o.projects.Transition(ctx, p.ID, domain.StatePlanning)
}

// scriptStage is PLANNING: Producer → Writer make a script version; the script checkpoint decides
// whether the user must approve it before the Project moves to VISUAL_DESIGN.
func (o *Orchestrator) scriptStage(ctx context.Context, p domain.Project) (bool, error) {
	latest, err := o.plans.LatestScript(ctx, p.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return true, o.writeScript(ctx, p, nil, "")
	}
	if err != nil {
		return false, err
	}

	cp := domain.CheckpointScript
	card, err := o.chatRepo.LatestApprovalCard(ctx, p.ID, cp.RefType(), latest.ID)
	if err != nil {
		return false, err
	}
	switch {
	case card == nil:
		brief, err := o.projects.GetBrief(ctx, p.ID)
		if err != nil {
			return false, err
		}
		posted, err := o.chat.RequestApproval(ctx, p.ID, cp, latest.Summary(brief.Duration), latest.ID)
		if err != nil {
			return false, err
		}
		if posted {
			return true, nil // AWAITING_APPROVAL: nothing runs until the card is answered
		}
		return true, o.projects.Transition(ctx, p.ID, domain.StateVisualDesign)
	case card.ApprovalStatus == domain.ApprovalApproved:
		return true, o.projects.Transition(ctx, p.ID, domain.StateVisualDesign)
	case card.ApprovalStatus == domain.ApprovalChangesRequested:
		feedback, err := o.chatRepo.LatestUserReply(ctx, p.ID, card.ID)
		if err != nil {
			return false, err
		}
		return true, o.writeScript(ctx, p, &latest, feedback)
	}
	return false, nil // the card is pending
}

// writeScript runs Producer and Writer and stores the result as the next script version.
// previous and feedback are set when the user asked for changes.
func (o *Orchestrator) writeScript(ctx context.Context, p domain.Project, previous *domain.ScriptVersion, feedback string) error {
	brief, err := o.projects.GetBrief(ctx, p.ID)
	if err != nil {
		return err
	}
	cin := planning.ConceptInput{Brief: brief, Template: p.ProductionTemplate, Feedback: feedback}
	if previous != nil {
		cin.Previous = &previous.Doc.Concept
	}
	concept, err := o.producer.Develop(ctx, cin)
	if err != nil {
		return err
	}
	notes, err := o.projectNotes(ctx, p.ID, brief, feedback)
	if err != nil {
		return err
	}
	sin := planning.ScriptInput{Brief: brief, Template: p.ProductionTemplate, Concept: concept, Feedback: feedback, Notes: notes}
	if previous != nil {
		sin.Previous = &previous.Doc.Script
	}
	script, err := o.writer.Write(ctx, sin)
	if err != nil {
		return err
	}
	_, err = o.plans.SaveScript(ctx, p.ID, domain.ScriptDoc{Concept: concept, Script: script})
	return err
}

// shotPlanStage is VISUAL_DESIGN before any Scene exists: Director → Storyboard plan the Scenes and
// Shots of the approved script.
func (o *Orchestrator) shotPlanStage(ctx context.Context, p domain.Project) (bool, error) {
	has, err := o.plans.HasScenes(ctx, p.ID)
	if err != nil || has {
		return false, err
	}
	script, err := o.plans.LatestScript(ctx, p.ID)
	if err != nil {
		return false, fmt.Errorf("no script for the Scene plan: %w", err)
	}
	brief, err := o.projects.GetBrief(ctx, p.ID)
	if err != nil {
		return false, err
	}
	notes, err := o.projectNotes(ctx, p.ID, brief, script.Doc.Script.Title)
	if err != nil {
		return false, err
	}
	plan, err := o.director.Plan(ctx, planning.PlanInput{Brief: brief, Template: p.ProductionTemplate, Concept: script.Doc.Concept, Script: script.Doc.Script, Notes: notes})
	if err != nil {
		return false, err
	}
	board, err := o.storyboarder.Draw(ctx, planning.BoardInput{Brief: brief, Template: p.ProductionTemplate, Script: script.Doc.Script, Plan: plan})
	if err != nil {
		return false, err
	}
	if err := o.plans.SavePlan(ctx, p.ID, plan, board); err != nil {
		return false, err
	}
	done := fmt.Sprintf("Scene / Shot の計画ができました（%d Scene / %d Shot）。補助ビューの「Scene / Shot」で構成を確認できます。", len(plan.Scenes), plan.ShotCount())
	if err := o.chat.Announce(ctx, p.ID, assistantText(done)); err != nil {
		o.logger.Error("post plan notice failed", "project_id", p.ID, "err", err)
	}
	return false, nil // Art Director and the later stages take over from here
}

// projectNotes chooses the production notes the Writer or Director is given (spec/spec.md §9.4): the
// pinned ones, then the ones that match the Brief and extra text (the change request, the script
// title), within the context budget. Archived notes are never chosen.
func (o *Orchestrator) projectNotes(ctx context.Context, projectID string, brief domain.Brief, extra string) ([]domain.NoteView, error) {
	if o.notes == nil {
		return nil, nil
	}
	all, err := o.notes.Context(ctx, projectID)
	if err != nil {
		return nil, err
	}
	query := strings.Join([]string{brief.Subject, brief.Objective, brief.Mood, brief.AdditionalInstruction, extra}, "\n")
	return domain.SelectNotes(all, query, domain.NoteContextBudgetRunes), nil
}
