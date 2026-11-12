// Package orchestrator is the Orchestrator Agent: it drives the Core State Machine of a Project
// (spec/spec.md §22) and decides which Agent runs next (§20.1). This package runs the planning
// and the generation part of the AUTO pipeline (spec/spec.md §4.3):
//
//	DRAFT ──Brief complete──▶ PLANNING ──Producer → Writer──▶ script_version
//	        ──(script approval card, when the Confirmation Level asks)──▶ VISUAL_DESIGN
//	        ──Director → Storyboard──▶ scene / shot
//	        ──(no approval card: AUTO is the consent)──▶ Reference Pack → Shot generation
//
// Every move is persisted (repository.ProjectRepository.Transition) before the next Agent runs,
// and every step derives what is left to do from what is stored, so an interrupted run is
// resumed by running the same job again. The stages are in orchestrator/stages and the automatic start of the generation in orchestrator/genstage; this
// package is the job around them (one job per Project, rerun, the notice of a failure).
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator/genstage"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator/stages"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/characterdraft"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/keylock"
	"github.com/ousiassllc/mam/backend/internal/planadvice"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Jobs is the background job queue. *jobqueue.Queue implements it.
type Jobs interface {
	Enqueue(ctx context.Context, kind domain.JobKind, payload any) (domain.Job, error)
}

// Drafts makes the draft Characters of a Project from the images attached to its Brief, without an LLM (*characterdraft.Maker implements it).
type Drafts interface {
	Make(ctx context.Context, projectID string) (characterdraft.Result, error)
}

// Deps are the collaborators of an Orchestrator.
type Deps struct {
	Projects *repository.ProjectRepository
	Plans    *repository.PlanRepository
	ChatRepo *repository.ChatRepository
	Chat     stages.Chat
	Jobs     Jobs
	// LLM is the Reasoning LLM Adapter every planning Agent reasons through.
	LLM llm.Completer
	// Notes are the production notes of the Project; the Writer and Director are given the relevant
	// ones (spec/spec.md §9.4). Nil: no notes.
	Notes *repository.NoteRepository
	// Drafts is asked for the draft Characters when the pipeline stops for want of an LLM API key (#1022). Nil: none are made.
	Drafts Drafts
	// Facts counts the Reference Library items and the Characters of a Project, for the sentence that says what the main line waits for
	// after the Scene / Shot plan (#1108). Nil: the sentence leaves them out.
	Facts func(ctx context.Context, projectID string) (references, characters int, err error)
	// Gate starts the generation by itself after the Scene / Shot plan (spec/spec.md §4.3). Nil: off; the user starts each Shot.
	Gate   *genstage.Gate
	Logger *slog.Logger
	// QueuedWarnAfter is how long a plan job may stay queued before a warning is logged (zero: planadvice.QueuedAfter, the time after which its progress card says it has not started).
	QueuedWarnAfter time.Duration
}

// Orchestrator drives the planning pipeline. It is safe for concurrent use.
type Orchestrator struct {
	projects *repository.ProjectRepository
	stages   *stages.Pipeline
	drafts   Drafts
	chat     stages.Chat
	jobs     Jobs
	logger   *slog.Logger

	queuedWarnAfter time.Duration

	mu      sync.Mutex
	waiting map[string]*time.Timer // plan job ID -> the timer that warns when it is still queued; ended when a worker starts the job
	active  map[string]bool        // a plan job of the Project is queued or running
	rerun   map[string]bool        // something happened while that job was running
	locks   keylock.Locker         // one advance at a time per Project
}

// New returns an Orchestrator.
func New(d Deps) *Orchestrator {
	logger := d.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	warnAfter := d.QueuedWarnAfter
	if warnAfter <= 0 {
		warnAfter = planadvice.QueuedAfter
	}
	return &Orchestrator{
		projects: d.Projects, drafts: d.Drafts, chat: d.Chat, jobs: d.Jobs, logger: logger, queuedWarnAfter: warnAfter,
		stages: stages.New(stages.Deps{
			Projects: d.Projects, Plans: d.Plans, ChatRepo: d.ChatRepo, Chat: d.Chat, LLM: d.LLM, Notes: d.Notes, Facts: d.Facts, Gate: d.Gate, Logger: logger,
		}),
		waiting: make(map[string]*time.Timer),
		active:  make(map[string]bool),
		rerun:   make(map[string]bool),
	}
}

// planPayload is the payload of a JobPlan job.
type planPayload struct {
	ProjectID string `json:"project_id"`
}

// progressText is the content of the progress card of a plan job. The job plans, and it also carries the generation on (the Reference Packs and the
// Shots it starts), so the words do not say that the job is only the plan.
const progressText = "企画・脚本・Scene/Shot 計画と生成の進行を進めています"

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
	ready, err := o.stages.NeedsWork(ctx, p)
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
	o.watchQueued(projectID, job.ID)
	card := domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindProgress, Content: progressText, RefID: job.ID}
	if err := o.chat.Announce(ctx, projectID, card); err != nil {
		o.logger.Error("post progress card failed", "project_id", projectID, "job_id", job.ID, "err", err)
	}
	return nil
}

// watchQueued logs a warning when the job is still queued after queuedWarnAfter (every worker of the queue is busy, or the queue is not running): the progress card says the
// same to the user (planadvice.Waiting, #1107), and the log keeps it for whoever looks later.
func (o *Orchestrator) watchQueued(projectID, jobID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.waiting[jobID] = time.AfterFunc(o.queuedWarnAfter, func() {
		o.mu.Lock()
		_, stillQueued := o.waiting[jobID]
		delete(o.waiting, jobID)
		o.mu.Unlock()
		if stillQueued {
			o.logger.Warn("plan job has not started: it is still queued", "project_id", projectID, "job_id", jobID, "queued_for", o.queuedWarnAfter.String())
		}
	})
}

// started ends the watch of a job a worker has taken.
func (o *Orchestrator) started(jobID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if t, ok := o.waiting[jobID]; ok {
		t.Stop()
		delete(o.waiting, jobID)
	}
}

// Running (false on a nil Orchestrator: no pipeline) reports whether a plan job of the Project is queued or running: a Kick then adds no job and no Progress card of its own, the job already
// there looks at the Project once more before it ends.
func (o *Orchestrator) Running(projectID string) bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.active[projectID]
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
	o.started(job.ID)
	ctx = o.withFallbackNotice(ctx, id)
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
		if err := o.advanceRecovered(ctx, id); err != nil {
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
	fail := failure(err)
	attrs := []any{"project_id", projectID, "err", err}
	if fail.Code == apperr.CodeUnknown {
		fail.ID = apperr.NewID() // the notice says it, the log line has it
		attrs = append(attrs, "error_id", fail.ID)
	}
	o.logger.Error("planning pipeline failed", attrs...)
	reason := failureReason(fail)
	if errors.Is(err, llm.ErrNoAPIKey) {
		reason += o.draftNote(ctx, projectID)
	}
	if fail.ID != "" {
		reason += "（" + apperr.Text(apperr.LangJa, "ui.error_id", nil) + ": " + fail.ID + "）"
	}
	notice := failmsg.Notice(err, "制作を続けられませんでした: "+reason+"\n対処したら、何かメッセージを送ると続きから再開します。") // a missing key links to the LLM assignment
	if aerr := o.chat.Announce(context.WithoutCancel(ctx), projectID, notice); aerr != nil {
		o.logger.Error("post failure notice failed", "project_id", projectID, "err", aerr)
	}
	return apperr.WithText(reason, fail)
}

// draftNote makes the draft Characters from the images attached to the Brief of a Project whose planning stopped for want of an LLM API
// key (a Project with no Characters has nothing to go on with, and the images would be unused) and returns the sentence that tells it
// for the failure reason ("" when there is no draft). A failure to make them is logged and never hides the failure of the planning.
func (o *Orchestrator) draftNote(ctx context.Context, projectID string) string {
	if o.drafts == nil {
		return ""
	}
	res, err := o.drafts.Make(context.WithoutCancel(ctx), projectID)
	if err != nil {
		o.logger.Error("make character drafts failed", "project_id", projectID, "err", err)
		return ""
	}
	if res.Drafts == 0 {
		return ""
	}
	return "\n" + planadvice.DraftNote(res.Drafts)
}

func assistantText(content string) domain.ChatMessage {
	return domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: content}
}
