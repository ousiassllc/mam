// Package stages is the stage logic of the planning pipeline (spec/spec.md §4.3): from the state a Project is stored in it
// decides what is left to do (needs work?) and runs the stages until it has to wait for the user, has nothing more to do or
// fails (Advance): DRAFT → PLANNING (Producer → Writer, script approval) → VISUAL_DESIGN (Director → Storyboard). The job
// bookkeeping around it (one job per Project, rerun, failure notice) is the orchestrator's. It is a package of its own so that
// orchestrator stays inside its line limit.
package stages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator/genstage"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Chat is what the stages need from the chat thread. *service.ChatService implements it.
type Chat interface {
	// RequestApproval posts the Approval card of a checkpoint when the Confirmation Level asks
	// for one and holds the Project in AWAITING_APPROVAL. It reports whether a card was posted.
	RequestApproval(ctx context.Context, projectID string, cp domain.Checkpoint, summary, refID string) (bool, error)
	// Announce posts an AI message that does not block the Project (text or progress card).
	Announce(ctx context.Context, projectID string, m domain.ChatMessage) error
}

// Deps are the collaborators of a Pipeline.
type Deps struct {
	Projects *repository.ProjectRepository
	Plans    *repository.PlanRepository
	ChatRepo *repository.ChatRepository
	Chat     Chat
	// LLM is the Reasoning LLM Adapter every planning Agent reasons through.
	LLM llm.Completer
	// Notes are the production notes of the Project; the Writer and Director are given the relevant ones (spec/spec.md §9.4). Nil: no notes.
	Notes *repository.NoteRepository
	// Facts counts the Reference Library items and the Characters of a Project, for the sentence that says what the main line waits for
	// after the Scene / Shot plan (#1108). Nil: the sentence leaves them out.
	Facts func(ctx context.Context, projectID string) (references, characters int, err error)
	// Gate is the generation approval that follows the Scene / Shot plan (spec/spec.md §4.3). Nil: off; the user starts each Shot.
	Gate   *genstage.Gate
	Logger *slog.Logger
}

// Pipeline runs the planning stages. It holds no per-Project state: every step derives what is left to do from what is stored.
type Pipeline struct {
	projects *repository.ProjectRepository
	plans    *repository.PlanRepository
	chatRepo *repository.ChatRepository
	notes    *repository.NoteRepository
	facts    func(ctx context.Context, projectID string) (references, characters int, err error)
	chat     Chat
	gate     *genstage.Gate
	logger   *slog.Logger

	producer     *planning.Producer
	writer       *planning.Writer
	director     *planning.Director
	storyboarder *planning.Storyboarder
}

// New returns a Pipeline. Deps.Logger must be set.
func New(d Deps) *Pipeline {
	return &Pipeline{
		projects: d.Projects, plans: d.Plans, chatRepo: d.ChatRepo, notes: d.Notes, facts: d.Facts, chat: d.Chat, gate: d.Gate, logger: d.Logger,
		producer:     planning.NewProducer(d.LLM),
		writer:       planning.NewWriter(d.LLM),
		director:     planning.NewDirector(d.LLM),
		storyboarder: planning.NewStoryboarder(d.LLM),
	}
}

// NeedsWork reports whether the pipeline has anything to do for the Project right now.
func (s *Pipeline) NeedsWork(ctx context.Context, p domain.Project) (bool, error) {
	if p.Mode != domain.ModeAuto {
		return false, nil
	}
	switch p.State {
	case domain.StateDraft:
		brief, err := s.projects.GetBrief(ctx, p.ID)
		if err != nil {
			return false, err
		}
		return len(brief.Missing()) == 0, nil
	case domain.StatePlanning:
		return true, nil
	case domain.StateVisualDesign:
		has, err := s.plans.HasScenes(ctx, p.ID)
		if err != nil || !has {
			return !has, err
		}
		return s.gate.NeedsWork(ctx, p)
	case domain.StateGenerating, domain.StateJudging, domain.StateRepairing:
		return s.gate.NeedsWork(ctx, p) // Shots the user approved are being made: the gate starts what is left
	}
	return false, nil
}

// Advance runs the pipeline from the Project's current state until it has to wait for the
// user (an Approval card), has nothing more to do, or fails.
func (s *Pipeline) Advance(ctx context.Context, projectID string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p, err := s.projects.Get(ctx, projectID)
		if err != nil {
			return err
		}
		if p.Mode != domain.ModeAuto {
			return nil // DIRECTOR MODE: the user drives
		}
		var progressed bool
		switch p.State {
		case domain.StateDraft:
			progressed, err = s.startPlanning(ctx, p)
		case domain.StatePlanning:
			progressed, err = s.scriptStage(ctx, p)
		case domain.StateVisualDesign:
			progressed, err = s.shotPlanStage(ctx, p)
		case domain.StateGenerating, domain.StateJudging, domain.StateRepairing:
			progressed, err = s.gate.Step(ctx, p)
		}
		if err != nil || !progressed {
			return err
		}
	}
}

// startPlanning moves a DRAFT Project with a complete Brief to PLANNING.
func (s *Pipeline) startPlanning(ctx context.Context, p domain.Project) (bool, error) {
	brief, err := s.projects.GetBrief(ctx, p.ID)
	if err != nil {
		return false, err
	}
	if len(brief.Missing()) > 0 {
		return false, nil
	}
	return true, s.projects.Transition(ctx, p.ID, domain.StatePlanning)
}

// subjectNotice is the question that holds the script back until the Brief says what the video is about.
const subjectNotice = "Brief の Subject（題材）がまだ決まっていません。決まるまで脚本は作りません。何（商品・題材）を主役にするか、返信か Brief の Subject で教えてください。"

// subjectKnown reports whether the Brief names what the video is about (domain.Brief.Missing). When it does not, it asks with a
// Subject question card (once: while that card is open nothing is posted again) and the caller stops: a script written without a
// topic is about something the user never asked for (#1108).
func (s *Pipeline) subjectKnown(ctx context.Context, projectID string) (bool, error) {
	brief, err := s.projects.GetBrief(ctx, projectID)
	if err != nil || !slices.Contains(brief.Missing(), domain.BriefSubject) {
		return err == nil, err
	}
	open, err := s.chatRepo.LatestOpenQuestion(ctx, projectID)
	if err != nil || (open != nil && open.RefID == string(domain.BriefSubject)) {
		return false, err
	}
	return false, s.chat.Announce(ctx, projectID, briefing.Question(domain.BriefSubject, subjectNotice+"\n"+domain.BriefSubject.Question()))
}

// scriptStage is PLANNING: Producer → Writer make a script version; the script checkpoint decides
// whether the user must approve it before the Project moves to VISUAL_DESIGN.
func (s *Pipeline) scriptStage(ctx context.Context, p domain.Project) (bool, error) {
	latest, err := s.plans.LatestScript(ctx, p.ID)
	if errors.Is(err, repository.ErrNotFound) {
		if known, err := s.subjectKnown(ctx, p.ID); err != nil || !known {
			return false, err // no script is written about a topic the user never named
		}
		return true, s.writeScript(ctx, p, nil, "")
	}
	if err != nil {
		return false, err
	}

	cp := domain.CheckpointScript
	card, err := s.chatRepo.LatestApprovalCard(ctx, p.ID, cp.RefType(), latest.ID)
	if err != nil {
		return false, err
	}
	switch {
	case card == nil:
		brief, err := s.projects.GetBrief(ctx, p.ID)
		if err != nil {
			return false, err
		}
		posted, err := s.chat.RequestApproval(ctx, p.ID, cp, latest.Summary(brief.Duration), latest.ID)
		if err != nil {
			return false, err
		}
		if posted {
			return true, nil // AWAITING_APPROVAL: nothing runs until the card is answered
		}
		return true, s.projects.Transition(ctx, p.ID, domain.StateVisualDesign)
	case card.ApprovalStatus == domain.ApprovalApproved:
		return true, s.projects.Transition(ctx, p.ID, domain.StateVisualDesign)
	case card.ApprovalStatus == domain.ApprovalChangesRequested:
		feedback, err := s.chatRepo.LatestUserReply(ctx, p.ID, card.ID)
		if err != nil {
			return false, err
		}
		return true, s.writeScript(ctx, p, &latest, feedback)
	}
	return false, nil // the card is pending
}

// writeScript runs Producer and Writer and stores the result as the next script version.
// previous and feedback are set when the user asked for changes.
func (s *Pipeline) writeScript(ctx context.Context, p domain.Project, previous *domain.ScriptVersion, feedback string) error {
	brief, err := s.projects.GetBrief(ctx, p.ID)
	if err != nil {
		return err
	}
	cin := planning.ConceptInput{Brief: brief, Template: p.ProductionTemplate, Feedback: feedback}
	if previous != nil {
		cin.Previous = &previous.Doc.Concept
	}
	concept, err := s.producer.Develop(ctx, cin)
	if err != nil {
		return err
	}
	notes, err := s.projectNotes(ctx, p.ID, brief, feedback)
	if err != nil {
		return err
	}
	sin := planning.ScriptInput{Brief: brief, Template: p.ProductionTemplate, Concept: concept, Feedback: feedback, Notes: notes}
	if previous != nil {
		sin.Previous = &previous.Doc.Script
	}
	script, err := s.writer.Write(ctx, sin)
	if err != nil {
		return err
	}
	_, err = s.plans.SaveScript(ctx, p.ID, domain.ScriptDoc{Concept: concept, Script: script})
	return err
}

// shotPlanStage is VISUAL_DESIGN: before any Scene exists, Director → Storyboard plan the Scenes and Shots of the approved script; once they
// exist, the generation approval (genstage) takes over.
func (s *Pipeline) shotPlanStage(ctx context.Context, p domain.Project) (bool, error) {
	has, err := s.plans.HasScenes(ctx, p.ID)
	if err != nil {
		return false, err
	}
	if has {
		return s.gate.Step(ctx, p)
	}
	script, err := s.plans.LatestScript(ctx, p.ID)
	if err != nil {
		return false, fmt.Errorf("no script for the Scene plan: %w", err)
	}
	brief, err := s.projects.GetBrief(ctx, p.ID)
	if err != nil {
		return false, err
	}
	notes, err := s.projectNotes(ctx, p.ID, brief, script.Doc.Script.Title)
	if err != nil {
		return false, err
	}
	plan, err := s.director.Plan(ctx, planning.PlanInput{Brief: brief, Template: p.ProductionTemplate, Concept: script.Doc.Concept, Script: script.Doc.Script, Notes: notes})
	if err != nil {
		return false, err
	}
	board, err := s.storyboarder.Draw(ctx, planning.BoardInput{Brief: brief, Template: p.ProductionTemplate, Script: script.Doc.Script, Plan: plan})
	if err != nil {
		return false, err
	}
	if err := s.plans.SavePlan(ctx, p.ID, plan, board); err != nil {
		return false, err
	}
	done := fmt.Sprintf("Scene / Shot の計画ができました（%d Scene / %d Shot）。補助ビューの「Scene / Shot」で構成を確認できます。", len(plan.Scenes), plan.ShotCount())
	if err := s.chat.Announce(ctx, p.ID, domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: done + "\n" + s.mainLineWait(ctx, p.ID, plan.ShotCount()).Text()}); err != nil {
		s.logger.Error("post plan notice failed", "project_id", p.ID, "err", err)
	}
	return true, nil // the generation approval is next (the gate does nothing while it is off: the user starts each Shot)
}

// projectNotes chooses the production notes the Writer or Director is given (spec/spec.md §9.4): the
// pinned ones, then the ones that match the Brief and extra text (the change request, the script
// title), within the context budget. Archived notes are never chosen.
func (s *Pipeline) projectNotes(ctx context.Context, projectID string, brief domain.Brief, extra string) ([]domain.NoteView, error) {
	if s.notes == nil {
		return nil, nil
	}
	all, err := s.notes.Context(ctx, projectID)
	if err != nil {
		return nil, err
	}
	query := strings.Join([]string{brief.Subject, brief.Objective, brief.Mood, brief.AdditionalInstruction, extra}, "\n")
	return domain.SelectNotes(all, query, domain.NoteContextBudgetRunes), nil
}
