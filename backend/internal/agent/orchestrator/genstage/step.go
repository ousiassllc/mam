package genstage

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
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

// announce tells the user once what the Orchestrator has started, for the Shots it started for the first time: a Shot that has a launch already
// belongs to an announcement made before, so looking again, a restart and the generation that follows a Reference Pack post nothing more. It is posted
// after the starts of the round and only when one was made: a look whose every start ended busy or gone (the claims were given back) announces nothing,
// so the announcement is made at most once for a start that was made. A process that ended between a start and this post leaves the start unannounced.
func (g *Gate) announce(ctx context.Context, p domain.Project, a Assessment, started []Step) {
	first := map[string]bool{}
	for _, s := range started {
		if len(a.Launches[s.Line.ShotID]) == 0 {
			first[s.Line.ShotID] = true
		}
	}
	if len(first) == 0 {
		return
	}
	plan := a.Plan.Only(func(l genplan.Line) bool { return first[l.ShotID] })
	msg := domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: plan.AnnounceText()}
	if err := g.d.Chat.Announce(ctx, p.ID, msg); err != nil {
		g.d.Logger.Error("post the announcement of the automatic start failed", "project_id", p.ID, "err", err)
	}
}

// refused is a start the budget ceiling or an error stopped, for the notice.
type refused struct {
	step Step
	err  error
}

// round is what the starts of one look came to.
type round struct {
	started []Step
	// budget is the start the budget ceiling refused and noModel the start refused because no model can be used: each ends the round. failed are the
	// starts that failed for another reason, each its own.
	budget, noModel *refused
	failed          []refused
}

// launch makes the starts of the plan. Each is claimed first: a claim that exists already (a look before, a process before) is skipped, so a Shot is
// never started twice. A Shot that is busy (started by hand) is left for the next look. The first start the budget ceiling refuses ends the round:
// nothing more is started until the user answers the Escalation. So does the first start refused because no model can be used
// (modelroute.ErrNoUsableModel): that is the state of the models and not of the Shot, so the Shots after it would be refused for the same cause;
// they stay unclaimed for a later look.
func (g *Gate) launch(ctx context.Context, p domain.Project, a Assessment) (bool, error) {
	var r round
	err := g.run(ctx, p, a, &r)
	done := context.WithoutCancel(ctx) // what was started is announced and what was refused is told, even when the job is cancelled
	g.announce(done, p, a, r.started)
	if rerr := g.report(done, p, r); err == nil {
		err = rerr
	}
	return len(r.started) > 0, err
}

// run makes the starts of the plan into r until one of them ends the round.
func (g *Gate) run(ctx context.Context, p domain.Project, a Assessment, r *round) error {
	for _, s := range a.Next {
		if err := ctx.Err(); err != nil {
			return err
		}
		claimed, err := g.d.Repo.Claim(ctx, s.Line.ShotID, s.Stage)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		job, err := g.start(ctx, s)
		switch {
		case err == nil:
			r.started = append(r.started, s)
			g.noModelCleared(p.ID)
			if err := g.d.Repo.Started(ctx, s.Line.ShotID, s.Stage, job.ID); err != nil {
				return err
			}
		case ctx.Err() != nil || errors.Is(err, generation.ErrShotBusy) || errors.Is(err, repository.ErrNotFound):
			if rerr := g.d.Repo.Release(context.WithoutCancel(ctx), s.Line.ShotID, s.Stage); rerr != nil {
				return rerr
			}
		case errors.Is(err, generation.ErrBudgetExceeded):
			r.budget = &refused{s, err}
			return nil
		case errors.Is(err, modelroute.ErrNoUsableModel):
			if g.noModelTold(p.ID, err) { // the user was told this cause and nothing changed since: the Shot is not marked, a later look tries it again
				return g.d.Repo.Release(context.WithoutCancel(ctx), s.Line.ShotID, s.Stage)
			}
			r.noModel = &refused{s, err}
			return nil
		default:
			r.failed = append(r.failed, refused{s, err})
		}
	}
	return nil
}

func (g *Gate) start(ctx context.Context, s Step) (domain.Job, error) {
	if s.Stage == genapprovalrepo.StageReferencePack {
		return g.gen.StartReferencePack(ctx, s.Line.ShotID)
	}
	return g.gen.StartByPolicy(ctx, s.Line.ShotID, "", nil)
}

// report tells the user about the starts that did not happen and keeps them from being tried again by themselves: the budget ceiling goes
// to an Escalation (like the Retry Policy's limit), anything else to a notice. The Shot is left to the user. A start that was refused for a model
// that cannot be used (the preflight of the Generation Orchestrator) is reported with its reason like any other failed start, but once for the round
// (the Shots after it were not tried) and, for the same cause, once until a start succeeds (noModelTold).
func (g *Gate) report(ctx context.Context, p domain.Project, r round) error {
	for _, f := range r.failed {
		if err := g.markFailed(ctx, p, f); err != nil {
			return err
		}
		g.tell(ctx, p, fmt.Sprintf("%s を自動では始められませんでした: %s\nこの Shot は自動では進めません。Shot を選んで手動で起動してください。", f.step.name(), generation.FailureReason(f.err)))
	}
	if r.noModel != nil {
		if err := g.markFailed(ctx, p, *r.noModel); err != nil {
			return err
		}
		g.noModelTell(p.ID, r.noModel.err)
		g.tell(ctx, p, fmt.Sprintf("%s を自動では始められませんでした: %s\nこの Shot は自動では進めません。Shot を選んで手動で起動してください。\n"+
			"これは Shot ではなくモデルの状態が原因なので、ほかの Shot はまだ始めていません。モデルを使えるようにすると（API キーの登録・割り当ての変更）、次の確認で自動で続けます。同じ原因のお知らせは繰り返しません。",
			r.noModel.step.name(), generation.FailureReason(r.noModel.err)))
	}
	if r.budget == nil {
		return nil
	}
	if err := g.d.Repo.Stopped(ctx, r.budget.step.Line.ShotID, r.budget.step.Stage, genapprovalrepo.OutcomeBudget); err != nil {
		return err
	}
	text := fmt.Sprintf("AUTO で進めている生成のうち、次は予算上限に達するため始めませんでした: %s。\n予算上限は AUTO を選んでいても効いています。この相談に答えるまで、ほかの Shot も新しくは始めません。", r.budget.step.name())
	options := []string{
		"設定で予算上限を引き上げてから、Shot を選んで手動で生成する",
		"Shot を減らす・Quality Policy を下げるなど、計画を直してから進める",
		"この Shot は作らない",
	}
	return g.d.Chat.RaiseEscalation(ctx, p.ID, text, options, "")
}

// markFailed records that the start of f failed, so that it is not tried again by itself.
func (g *Gate) markFailed(ctx context.Context, p domain.Project, f refused) error {
	if err := g.d.Repo.Stopped(ctx, f.step.Line.ShotID, f.step.Stage, genapprovalrepo.OutcomeFailed); err != nil {
		return err
	}
	g.d.Logger.Error("start a generation by itself failed", "project_id", p.ID, "shot_id", f.step.Line.ShotID, "stage", f.step.Stage, "err", f.err)
	return nil
}

// tell posts a notice of the assistant; a notice that could not be posted is logged (the starts it is about were recorded).
func (g *Gate) tell(ctx context.Context, p domain.Project, text string) {
	if err := g.d.Chat.Announce(ctx, p.ID, domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: text}); err != nil {
		g.d.Logger.Error("post the notice of a failed start failed", "project_id", p.ID, "err", err)
	}
}
