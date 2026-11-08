package genstage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// SupersededNotice is what the chat says when the Orchestrator settles a generation approval card of v0.1.34 that was still pending: the card is closed
// by the system, never as the user's answer.
const SupersededNotice = "旧方針（v0.1.34）の「生成の承認」カードが未回答のまま残っていました。AUTO では生成の承認を求めない仕様に変わったため、システムがこのカードを閉じました" +
	"（あなたの回答ではありません）。AUTO を選んでいるので、続けて生成を自動で始めます。止めるには、設定で予算上限を決めるか、MODE を DIRECTOR にしてください。"

// Step looks at the Project once and does what the gate has to do: it settles a legacy card that is still pending, or starts the Reference Packs and
// Shots of the plan. It reports whether it did something that can leave more to do (the caller looks again).
func (g *Gate) Step(ctx context.Context, p domain.Project) (bool, error) {
	a, err := g.Assess(ctx, p)
	if err != nil {
		return false, err
	}
	switch {
	case a.Verdict == VerdictSupersede:
		_, err := g.d.Chat.Supersede(ctx, p.ID, a.Card.ID, SupersededNotice) // false: the user answered it meanwhile; either way the next look goes on
		return true, err
	case a.Verdict == VerdictStart && len(a.Next) > 0:
		return g.launch(ctx, p, a)
	}
	return false, nil
}

// announce tells the user once what the Orchestrator is about to start, for the Shots it starts for the first time: a Shot that has a launch already
// belongs to an announcement made before, so looking again, a restart and the generation that follows a Reference Pack post nothing more. It is posted
// before the first claim: a process that ended in between announces again, which is better than a generation nobody was told about.
func (g *Gate) announce(ctx context.Context, p domain.Project, a Assessment) error {
	first := map[string]bool{}
	for _, s := range a.Next {
		if len(a.Launches[s.Line.ShotID]) == 0 {
			first[s.Line.ShotID] = true
		}
	}
	if len(first) == 0 {
		return nil
	}
	plan := a.Plan.Only(func(l genplan.Line) bool { return first[l.ShotID] })
	return g.d.Chat.Announce(ctx, p.ID, domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: plan.AnnounceText()})
}

// refused is a start the budget ceiling or an error stopped, for the notice.
type refused struct {
	step Step
	err  error
}

// launch makes the starts of the plan. Each is claimed first: a claim that exists already (a look before, a process before) is skipped, so a Shot is
// never started twice. A Shot that is busy (started by hand) is left for the next look. The first start the budget ceiling refuses ends the round:
// nothing more is started until the user answers the Escalation.
func (g *Gate) launch(ctx context.Context, p domain.Project, a Assessment) (bool, error) {
	if err := g.announce(ctx, p, a); err != nil {
		return false, err
	}
	var started bool
	var budget, failed []refused
launch:
	for _, s := range a.Next {
		if err := ctx.Err(); err != nil {
			return started, err
		}
		claimed, err := g.d.Repo.Claim(ctx, s.Line.ShotID, s.Stage)
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
			break launch
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
// to an Escalation (like the Retry Policy's limit), anything else to a notice. The Shot is left to the user. A start that was refused for a model
// that cannot be used (the preflight of the Generation Orchestrator) is reported with its reason like any other failed start.
func (g *Gate) report(ctx context.Context, p domain.Project, budget, failed []refused) error {
	ctx = context.WithoutCancel(ctx)
	for _, r := range failed {
		if err := g.d.Repo.Stopped(ctx, r.step.Line.ShotID, r.step.Stage, genapprovalrepo.OutcomeFailed); err != nil {
			return err
		}
		g.d.Logger.Error("start a generation by itself failed", "project_id", p.ID, "shot_id", r.step.Line.ShotID, "stage", r.step.Stage, "err", r.err)
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
	text := fmt.Sprintf("AUTO で進めている生成のうち、次は予算上限に達するため始めませんでした: %s。\n予算上限は AUTO を選んでいても効いています。この相談に答えるまで、ほかの Shot も新しくは始めません。", strings.Join(names, "、"))
	options := []string{
		"設定で予算上限を引き上げてから、Shot を選んで手動で生成する",
		"Shot を減らす・Quality Policy を下げるなど、計画を直してから進める",
		"この Shot は作らない",
	}
	return g.d.Chat.RaiseEscalation(ctx, p.ID, text, options, "")
}
