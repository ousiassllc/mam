package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

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

// subjectNotice is the question that holds the script back until the Brief says what the video is about.
const subjectNotice = "Brief の Subject（題材）がまだ決まっていません。決まるまで脚本は作りません。何（商品・題材）を主役にするか、返信か Brief の Subject で教えてください。"

// subjectKnown reports whether the Brief names what the video is about (domain.Brief.Missing). When it does not, it asks with a
// Subject question card (once: while that card is open nothing is posted again) and the caller stops: a script written without a
// topic is about something the user never asked for (#1108).
func (o *Orchestrator) subjectKnown(ctx context.Context, projectID string) (bool, error) {
	brief, err := o.projects.GetBrief(ctx, projectID)
	if err != nil || !slices.Contains(brief.Missing(), domain.BriefSubject) {
		return err == nil, err
	}
	open, err := o.chatRepo.LatestOpenQuestion(ctx, projectID)
	if err != nil || (open != nil && open.RefID == string(domain.BriefSubject)) {
		return false, err
	}
	return false, o.chat.Announce(ctx, projectID, briefing.Question(domain.BriefSubject, subjectNotice+"\n"+domain.BriefSubject.Question()))
}

// scriptStage is PLANNING: Producer → Writer make a script version; the script checkpoint decides
// whether the user must approve it before the Project moves to VISUAL_DESIGN.
func (o *Orchestrator) scriptStage(ctx context.Context, p domain.Project) (bool, error) {
	latest, err := o.plans.LatestScript(ctx, p.ID)
	if errors.Is(err, repository.ErrNotFound) {
		if known, err := o.subjectKnown(ctx, p.ID); err != nil || !known {
			return false, err // no script is written about a topic the user never named
		}
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
	if err := o.chat.Announce(ctx, p.ID, assistantText(done+"\n"+o.mainLineWait(ctx, p.ID, plan.ShotCount()).Text())); err != nil {
		o.logger.Error("post plan notice failed", "project_id", p.ID, "err", err)
	}
	return false, nil // generation is started per Shot by the user (a generation costs money): the notice says what to press
}

// mainLineWait is what the main line waits for once shots Shots are planned. A failure to count the Library and the Characters is logged and
// leaves them out of the sentence: the wait reason is told either way.
func (o *Orchestrator) mainLineWait(ctx context.Context, projectID string, shots int) mainline.Wait {
	wait := mainline.Wait{Shots: shots}
	if o.facts == nil {
		return wait
	}
	refs, chars, err := o.facts(ctx, projectID)
	if err != nil {
		o.logger.Error("count references and characters failed", "project_id", projectID, "err", err)
		return wait
	}
	wait.Counted, wait.References, wait.Characters = true, refs, chars
	return wait
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
