package genstage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// Verdict is what the gate finds when it looks at a Project.
type Verdict int

// Verdicts.
const (
	VerdictNone      Verdict = iota // nothing to start (not AUTO, no Shot left, the gate is off)
	VerdictSupersede                // a generation card of v0.1.34 is still pending: the gate settles it, then goes on
	VerdictDeclined                 // a card of v0.1.34 was declined: nothing starts until the user writes or the plan changes
	VerdictStart                    // the generation starts by itself: Next are the starts to make
)

// Step is one start the gate makes.
type Step struct {
	Line  genplan.Line
	Stage genapprovalrepo.Stage
}

// Assessment is what Assess found.
type Assessment struct {
	Verdict Verdict
	Plan    genplan.Plan
	// Card is the newest generation card of v0.1.34 (nil when there is none).
	Card     *genapprovalrepo.Card
	Launches map[string]map[genapprovalrepo.Stage]genapprovalrepo.Launch
	// Unrecorded is set with VerdictDeclined when the declined card has no record of what it showed: only a message of the user lifts that decline.
	Unrecorded bool
	// Next are the starts to make now (VerdictStart).
	Next []Step
	// Stuck holds the Shots the gate cannot carry on by itself: a start that was refused or failed, and a Reference Pack that failed.
	Stuck map[string]bool
}

// inScope reports whether the Project is at a state where the generation is the gate's business: the plan is made (VISUAL_DESIGN, where a Project stays
// while its Shots are made, spec/spec.md §22), a card is waiting (AWAITING_APPROVAL: the legacy card the gate settles, or an Escalation it waits for),
// or an Escalation was answered (REPAIRING: the budget stopped a start and the user decided). GENERATING and JUDGING are the Reshoot Loop's and the director's, never the gate's.
func inScope(s domain.ProjectState) bool {
	switch s {
	case domain.StateVisualDesign, domain.StateAwaitingApproval, domain.StateRepairing:
		return true
	}
	return false
}

// Assess looks at the Project and says what the gate has to do. It changes nothing.
func (g *Gate) Assess(ctx context.Context, p domain.Project) (Assessment, error) {
	if !g.On() || p.Mode != domain.ModeAuto || !inScope(p.State) {
		return Assessment{}, nil
	}
	card, err := g.d.Repo.LatestCard(ctx, p.ID)
	if err != nil {
		return Assessment{}, err
	}
	if card != nil && card.Status == domain.ApprovalPending {
		return Assessment{Verdict: VerdictSupersede, Card: card}, nil // an obsolete card holds the Project: nothing else is looked at before it is settled
	}
	launches, err := g.d.Repo.Launches(ctx, p.ID)
	if err != nil {
		return Assessment{}, err
	}
	plan, err := g.builder.Build(ctx, p.ID, launches)
	if err != nil {
		return Assessment{}, err
	}
	a := Assessment{Plan: plan, Card: card, Launches: launches, Stuck: map[string]bool{}}
	for shotID, stages := range launches {
		for _, l := range stages {
			a.Stuck[shotID] = a.Stuck[shotID] || l.Outcome != genapprovalrepo.OutcomeStarted
		}
	}
	if len(plan.Lines) == 0 || p.State == domain.StateAwaitingApproval {
		return a, nil // under an Escalation (it is the user's) nothing starts, but the Shots the gate refused or failed still show: the Project is AUTO
	}
	if card != nil && card.Status == domain.ApprovalChangesRequested {
		declined, err := g.declined(ctx, p, &a)
		if err != nil {
			return Assessment{}, err
		}
		if declined {
			a.Verdict = VerdictDeclined
			return a, nil
		}
	}
	a.Verdict = VerdictStart
	return a, g.next(ctx, &a)
}

// SupersededNotice is what the chat says when the Orchestrator settles a generation approval card of v0.1.34 that was still pending: the card is closed by the system, never as the user's answer.
const SupersededNotice = "旧方針（v0.1.34）の「生成の承認」カードが未回答のまま残っていました。AUTO では生成の承認を求めない仕様に変わったため、システムがこのカードを閉じました" +
	"（あなたの回答ではありません）。AUTO を選んでいるので、続けて生成を自動で始めます。" + mainline.BudgetCeilingScope + mainline.StopAuto

// declined reports whether the card the user declined (under the policy of v0.1.34) still keeps the generation from starting: it does until the
// plan changed from what the card showed, or the user wrote to the thread after the decline (not as a reply to the card). A card with no record of
// what it showed (an imported bundle carries the cards but not the records) is lifted by a message only: a.Unrecorded says so, for the words that tell the user what to do.
func (g *Gate) declined(ctx context.Context, p domain.Project, a *Assessment) (bool, error) {
	approved, ok, err := g.d.Repo.Approval(ctx, a.Card.ID)
	if err != nil {
		return false, err
	}
	a.Unrecorded = !ok || approved.ProjectID != p.ID
	if !a.Unrecorded {
		lines, err := genplan.ScopeLines(approved.Scope)
		if err != nil {
			return false, err
		}
		if genplan.Changed(lines, a.Plan) {
			return false, nil
		}
	}
	again, err := g.d.Repo.AskedAgainAfter(ctx, p.ID, a.Card.ID)
	return !again, err
}

// next lists the starts to make for the plan, in the order of the Shots. A Shot that needs a Reference Pack gets it first; its generation is started when that job has succeeded (a job that failed leaves the Shot to the user). A Shot that needs none is generated at once.
func (g *Gate) next(ctx context.Context, a *Assessment) error {
	for _, line := range a.Plan.Lines {
		pack, packClaimed := a.Launches[line.ShotID][genapprovalrepo.StageReferencePack]
		switch {
		case line.Pack:
			a.Next = append(a.Next, Step{line, genapprovalrepo.StageReferencePack})
		case !packClaimed:
			a.Next = append(a.Next, Step{line, genapprovalrepo.StageGenerate})
		default:
			ready, err := g.packDone(ctx, pack, line, a)
			if err != nil {
				return err
			}
			if ready {
				a.Next = append(a.Next, Step{line, genapprovalrepo.StageGenerate})
			}
		}
	}
	return nil
}

// packDone reports whether the Reference Pack of the line has been made, so that the Shot is generated next. A job that is still queued or
// running is waited for; one that failed marks the Shot as stuck (the user was told when it failed). A claim whose job was never recorded
// (the process ended between the claim and the start) counts as done: it is never started again, and the generation goes on.
func (g *Gate) packDone(ctx context.Context, pack genapprovalrepo.Launch, line genplan.Line, a *Assessment) (bool, error) {
	if pack.JobID == "" {
		return true, nil
	}
	job, err := g.d.Jobs.Get(ctx, pack.JobID)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return true, nil
	case err != nil:
		return false, err
	}
	switch job.Status {
	case domain.JobSucceeded:
		return true, nil
	case domain.JobFailed:
		a.Stuck[line.ShotID] = true
	}
	return false, nil
}

// NeedsWork reports whether the gate has anything to do for the Project right now: a legacy card to settle, or a start to make.
func (g *Gate) NeedsWork(ctx context.Context, p domain.Project) (bool, error) {
	a, err := g.Assess(ctx, p)
	return err == nil && (a.Verdict == VerdictSupersede || (a.Verdict == VerdictStart && len(a.Next) > 0)), err
}

// Phase is what the main line of the Project waits for, for the notice on the Scene / Shot screen, and the Shots that cannot be carried on by the gate.
func (g *Gate) Phase(ctx context.Context, p domain.Project) (mainline.Phase, map[string]bool, error) {
	a, err := g.Assess(ctx, p)
	if err != nil {
		return mainline.PhaseManual, nil, err
	}
	switch a.Verdict {
	case VerdictDeclined:
		if a.Unrecorded {
			return mainline.PhaseDeclinedUnrecorded, a.Stuck, nil
		}
		return mainline.PhaseDeclined, a.Stuck, nil
	case VerdictSupersede, VerdictStart:
		return mainline.PhaseAuto, a.Stuck, nil
	}
	if len(a.Launches) > 0 {
		return mainline.PhaseAuto, a.Stuck, nil // everything left was started or refused
	}
	return mainline.PhaseManual, a.Stuck, nil
}

// shotOf is the Shot of a generate job.
func shotOf(job domain.Job) (string, bool) {
	var p struct {
		ShotID string `json:"shot_id"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.ShotID == "" {
		return "", false
	}
	return p.ShotID, true
}

// name is how the chat names a start.
func (s Step) name() string {
	if s.Stage == genapprovalrepo.StageReferencePack {
		return fmt.Sprintf("Shot %d の Reference Pack", s.Line.Number)
	}
	return fmt.Sprintf("Shot %d の生成", s.Line.Number)
}
