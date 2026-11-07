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
	VerdictNone     Verdict = iota // nothing to approve or make (not AUTO, no Shot left, the gate is off)
	VerdictAsk                     // an approval card must be posted: there is none, or the one there is cannot stand for the plan
	VerdictPending                 // the card waits for an answer
	VerdictDeclined                // the card was declined and nothing asks again yet
	VerdictApproved                // the plan was approved: Next are the starts to make
)

// Step is one start the gate makes for an approved plan.
type Step struct {
	Line  genplan.Line
	Stage genapprovalrepo.Stage
}

// Assessment is what Assess found.
type Assessment struct {
	Verdict Verdict
	// Reason is why a new card is asked for ("" for the first card).
	Reason string
	Plan   genplan.Plan
	// Card is the newest generation card (nil when there is none).
	Card     *genapprovalrepo.Card
	Launches map[string]map[genapprovalrepo.Stage]genapprovalrepo.Launch
	// Next are the starts to make now (VerdictApproved).
	Next []Step
	// Stuck holds the Shots the gate cannot carry on by itself: a start that was refused or failed, and a Reference Pack that failed.
	Stuck map[string]bool
}

// inScope reports whether the Project is at a state where the generation is the gate's business: the plan is made (VISUAL_DESIGN), a card is
// waiting (AWAITING_APPROVAL) or Shots are being made (GENERATING, JUDGING, REPAIRING; a Project stays in VISUAL_DESIGN while they are, spec/spec.md §22).
func inScope(s domain.ProjectState) bool {
	switch s {
	case domain.StateVisualDesign, domain.StateAwaitingApproval, domain.StateGenerating, domain.StateJudging, domain.StateRepairing:
		return true
	}
	return false
}

// canAct reports whether the gate may post a card or start a Shot: not while the Project waits for the user at another card.
func canAct(s domain.ProjectState) bool { return inScope(s) && s != domain.StateAwaitingApproval }

// Assess looks at the Project and says what the gate has to do. It changes nothing.
func (g *Gate) Assess(ctx context.Context, p domain.Project) (Assessment, error) {
	if !g.On() || p.Mode != domain.ModeAuto || !inScope(p.State) {
		return Assessment{}, nil
	}
	launches, err := g.d.Repo.Launches(ctx, p.ID)
	if err != nil {
		return Assessment{}, err
	}
	plan, err := g.builder.Build(ctx, p.ID, launches)
	if err != nil {
		return Assessment{}, err
	}
	a := Assessment{Plan: plan, Launches: launches, Stuck: map[string]bool{}}
	for shotID, stages := range launches {
		for _, l := range stages {
			a.Stuck[shotID] = a.Stuck[shotID] || l.Outcome != genapprovalrepo.OutcomeStarted
		}
	}
	if len(plan.Lines) == 0 {
		return a, nil
	}
	if a.Card, err = g.d.Repo.LatestCard(ctx, p.ID); err != nil {
		return Assessment{}, err
	}
	if err := g.decide(ctx, p, &a); err != nil {
		return Assessment{}, err
	}
	return a, nil
}

// decide fills the verdict from the newest card.
func (g *Gate) decide(ctx context.Context, p domain.Project, a *Assessment) error {
	card := a.Card
	if card == nil {
		a.Verdict = VerdictAsk
		return nil
	}
	if card.Status == domain.ApprovalPending {
		a.Verdict = VerdictPending
		return nil
	}
	approved, ok, err := g.record(ctx, p.ID, card, a.Plan)
	if err != nil {
		return err
	}
	if !ok { // the card has no record of what it stood for and the plan is not what it was posted for: it cannot stand for the plan
		a.Verdict, a.Reason = VerdictAsk, "前の承認カードが何を承認するものか確かめられません"
		return nil
	}
	lines, err := genplan.ScopeLines(approved.Scope)
	if err != nil {
		return err
	}
	paid := genplan.Priced(lines, a.Plan)
	if card.Status == domain.ApprovalChangesRequested {
		return g.decideDeclined(ctx, p, a, lines, paid)
	}
	switch change, rise := genplan.Changed(lines, a.Plan), genplan.Increased(paid, a.Plan.Total()); {
	case change != "":
		a.Verdict, a.Reason = VerdictAsk, "承認のあとに計画が変わりました: "+change
	case rise != "":
		a.Verdict, a.Reason = VerdictAsk, rise
	default:
		a.Verdict = VerdictApproved
		return g.next(ctx, a)
	}
	return nil
}

// decideDeclined is the verdict for a card the user declined: it asks again when the plan or its price changed, or when the user wrote to the
// thread after the decline (not as a reply to the card), and waits otherwise.
func (g *Gate) decideDeclined(ctx context.Context, p domain.Project, a *Assessment, lines []genplan.Line, paid genplan.Cost) error {
	switch change := genplan.Changed(lines, a.Plan); {
	case change != "":
		a.Verdict, a.Reason = VerdictAsk, "差戻しのあとに計画が変わりました: "+change
	case genplan.Differs(paid, a.Plan.Total()):
		a.Verdict, a.Reason = VerdictAsk, "差戻しのあとに見積が変わりました"
	default:
		again, err := g.d.Repo.AskedAgainAfter(ctx, p.ID, a.Card.ID)
		if err != nil {
			return err
		}
		a.Verdict = VerdictDeclined
		if again {
			a.Verdict, a.Reason = VerdictAsk, "もう一度承認を求めます（差戻しのあとにメッセージが届きました）"
		}
	}
	return nil
}

// record is what the card stood for. A card posted just before a crash has none yet: it is written from the plan when the plan is the one
// the card was posted for (its fingerprint), and ok is false otherwise.
func (g *Gate) record(ctx context.Context, projectID string, card *genapprovalrepo.Card, plan genplan.Plan) (genapprovalrepo.Approval, bool, error) {
	a, ok, err := g.d.Repo.Approval(ctx, card.ID)
	if err != nil || ok {
		return a, ok, err
	}
	if card.Fingerprint != plan.Fingerprint() {
		return genapprovalrepo.Approval{}, false, nil
	}
	a = approvalOf(projectID, card.ID, plan)
	return a, true, g.d.Repo.SaveApproval(ctx, a)
}

func approvalOf(projectID, messageID string, plan genplan.Plan) genapprovalrepo.Approval {
	return genapprovalrepo.Approval{MessageID: messageID, ProjectID: projectID, Fingerprint: plan.Fingerprint(), Scope: plan.Scope()}
}

// next lists the starts to make for the approved plan, in the order of the Shots. A Shot that needs a Reference Pack gets it first; its
// generation is started when that job has succeeded (a job that failed leaves the Shot to the user). A Shot that needs none is generated at once.
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
// (the process ended between the claim and the start) counts as done: it is never started again, and the generation the user approved goes on.
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

// NeedsWork reports whether the gate has anything to do for the Project right now: a card to post, or a start to make.
func (g *Gate) NeedsWork(ctx context.Context, p domain.Project) (bool, error) {
	if !canAct(p.State) {
		return false, nil
	}
	a, err := g.Assess(ctx, p)
	return err == nil && (a.Verdict == VerdictAsk || len(a.Next) > 0), err
}

// Phase is what the main line of the Project waits for, for the notice on the Scene / Shot screen, and the Shots that cannot be carried on by
// the gate.
func (g *Gate) Phase(ctx context.Context, p domain.Project) (mainline.Phase, map[string]bool, error) {
	a, err := g.Assess(ctx, p)
	if err != nil {
		return mainline.PhaseManual, nil, err
	}
	switch a.Verdict {
	case VerdictAsk, VerdictPending:
		return mainline.PhaseApproval, a.Stuck, nil
	case VerdictDeclined:
		return mainline.PhaseDeclined, a.Stuck, nil
	case VerdictApproved:
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

// stepName is how the chat names a start.
func (s Step) name() string {
	if s.Stage == genapprovalrepo.StageReferencePack {
		return fmt.Sprintf("Shot %d の Reference Pack", s.Line.Number)
	}
	return fmt.Sprintf("Shot %d の生成", s.Line.Number)
}
