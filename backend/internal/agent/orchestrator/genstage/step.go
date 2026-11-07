package genstage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// Step looks at the Project once and does what the gate has to do: it posts the approval card, or starts the Reference Packs and Shots of an
// approved plan. It reports whether it did something that can leave more to do (the caller looks again).
func (g *Gate) Step(ctx context.Context, p domain.Project) (bool, error) {
	if !canAct(p.State) {
		return false, nil
	}
	a, err := g.Assess(ctx, p)
	if err != nil {
		return false, err
	}
	switch {
	case a.Verdict == VerdictAsk:
		return true, g.ask(ctx, p, a)
	case a.Verdict == VerdictApproved && len(a.Next) > 0:
		return g.launch(ctx, p, a)
	}
	return false, nil
}

// ask posts the approval card of the plan and records what it stands for. The card holds the Project in AWAITING_APPROVAL: nothing is started
// until it is answered, at any Confirmation Level.
func (g *Gate) ask(ctx context.Context, p domain.Project, a Assessment) error {
	fingerprint := a.Plan.Fingerprint()
	posted, err := g.d.Chat.RequestApproval(ctx, p.ID, domain.CheckpointGeneration, a.Plan.CardText(a.Reason), fingerprint)
	if err != nil {
		return err
	}
	if !posted {
		return errors.New("the generation approval card was not posted")
	}
	card, err := g.d.Repo.LatestCard(ctx, p.ID)
	if err != nil {
		return err
	}
	if card == nil || card.Fingerprint != fingerprint {
		return errors.New("the generation approval card that was posted is not the newest one")
	}
	return g.d.Repo.SaveApproval(ctx, approvalOf(p.ID, card.ID, a.Plan))
}

// refused is a start the budget ceiling or an error stopped, for the notice.
type refused struct {
	step Step
	err  error
}

// launch makes the starts of an approved plan. Each is claimed first: a claim that exists already (a look before, a process before) is skipped, so
// a Shot is never started twice for the approval. A Shot that is busy (started by hand) is left for the next look.
func (g *Gate) launch(ctx context.Context, p domain.Project, a Assessment) (bool, error) {
	var started bool
	var budget, failed []refused
	for _, s := range a.Next {
		if err := ctx.Err(); err != nil {
			return started, err
		}
		claimed, err := g.d.Repo.Claim(ctx, s.Line.ShotID, s.Stage, a.Card.ID)
		if err != nil {
			return started, err
		}
		if !claimed {
			continue
		}
		job, err := g.start(ctx, s)
		switch {
		case err == nil:
			started = true
			if err := g.d.Repo.Started(ctx, s.Line.ShotID, s.Stage, job.ID); err != nil {
				return started, err
			}
		case ctx.Err() != nil || errors.Is(err, generation.ErrShotBusy) || errors.Is(err, repository.ErrNotFound):
			if rerr := g.d.Repo.Release(context.WithoutCancel(ctx), s.Line.ShotID, s.Stage); rerr != nil {
				return started, rerr
			}
		case errors.Is(err, generation.ErrBudgetExceeded):
			budget = append(budget, refused{s, err})
		default:
			failed = append(failed, refused{s, err})
		}
	}
	return started, g.report(ctx, p, budget, failed)
}

func (g *Gate) start(ctx context.Context, s Step) (domain.Job, error) {
	if s.Stage == genapprovalrepo.StageReferencePack {
		return g.gen.StartReferencePack(ctx, s.Line.ShotID)
	}
	return g.gen.StartByPolicy(ctx, s.Line.ShotID, "", nil)
}

// report tells the user about the starts that did not happen and keeps them from being tried again by themselves: the budget ceiling goes
// to an Escalation (like the Retry Policy's limit), anything else to a notice. The Shot is left to the user.
func (g *Gate) report(ctx context.Context, p domain.Project, budget, failed []refused) error {
	ctx = context.WithoutCancel(ctx)
	for _, r := range failed {
		if err := g.d.Repo.Stopped(ctx, r.step.Line.ShotID, r.step.Stage, genapprovalrepo.OutcomeFailed); err != nil {
			return err
		}
		g.d.Logger.Error("start a generation after the approval failed", "project_id", p.ID, "shot_id", r.step.Line.ShotID, "stage", r.step.Stage, "err", r.err)
		text := fmt.Sprintf("%s を自動では始められませんでした: %s\nこの Shot は自動では進めません。Shot を選んで手動で起動してください。", r.step.name(), generation.FailureReason(r.err))
		if err := g.d.Chat.Announce(ctx, p.ID, domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: text}); err != nil {
			g.d.Logger.Error("post the notice of a failed start failed", "project_id", p.ID, "err", err)
		}
	}
	if len(budget) == 0 {
		return nil
	}
	names := make([]string, len(budget))
	for i, r := range budget {
		if err := g.d.Repo.Stopped(ctx, r.step.Line.ShotID, r.step.Stage, genapprovalrepo.OutcomeBudget); err != nil {
			return err
		}
		names[i] = r.step.name()
	}
	text := fmt.Sprintf("承認された生成のうち、次は予算上限に達するため始めませんでした: %s。\n上限は承認の代わりにはならず、承認のあとも効いています。", strings.Join(names, "、"))
	options := []string{
		"設定で予算上限を引き上げてから、Shot を選んで手動で生成する",
		"Shot を減らす・Quality Policy を下げるなど、計画を直してから進める",
		"この Shot は作らない",
	}
	return g.d.Chat.RaiseEscalation(ctx, p.ID, text, options, "")
}
