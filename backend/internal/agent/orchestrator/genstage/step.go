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
	// budget is the start the budget ceiling refused: it ends the round. noModel are the first starts of each cause (not told before) refused because no
	// model can be used, and causes all the causes the round met. failed are the starts that failed for another reason, each its own.
	budget  *refused
	noModel []refused
	causes  map[string]bool
	failed  []refused
}

// launch makes the starts of the plan. Each is claimed first: a claim that exists already (a look before, a process before) is skipped, so a Shot is
// never started twice. A Shot that is busy (started by hand) is left for the next look. The first start the budget ceiling refuses ends the round:
// nothing more is started until the user answers the Escalation. A start refused because no model can be used (modelroute.ErrNoUsableModel) is the
// state of the models and not of the Shot, so it does not end the round: a Shot that needs another purpose or media may still run. Of the Shots
// refused for one cause only the first is marked and told; the others are released, for a later look (nomodel.go).
func (g *Gate) launch(ctx context.Context, p domain.Project, a Assessment) (bool, error) {
	r := round{causes: map[string]bool{}}
	err := g.run(ctx, p, a, &r)
	switch {
	case len(r.causes) > 0:
		g.noModelRemember(p.ID, r.causes)
	case len(r.started) > 0:
		g.noModelCleared(p.ID)
	}
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
			if rerr := g.refuseNoModel(ctx, p, s, err, r); rerr != nil {
				return rerr
			}
		default:
			r.failed = append(r.failed, refused{s, err})
		}
	}
	return nil
}

// refuseNoModel handles a start refused because no model can be used. The first Shot of a cause that nobody was told yet is marked and told (report); a
// Shot refused for a cause that was told already, by a look before or by this round, is not marked: it is given its claim back and tried again later.
func (g *Gate) refuseNoModel(ctx context.Context, p domain.Project, s Step, err error, r *round) error {
	first := !r.causes[causeKey(err)]
	r.causes[causeKey(err)] = true
	if first && !g.noModelTold(p.ID, err) {
		r.noModel = append(r.noModel, refused{s, err})
		return nil
	}
	return g.d.Repo.Release(context.WithoutCancel(ctx), s.Line.ShotID, s.Stage)
}

func (g *Gate) start(ctx context.Context, s Step) (domain.Job, error) {
	if s.Stage == genapprovalrepo.StageReferencePack {
		return g.gen.StartReferencePack(ctx, s.Line.ShotID)
	}
	return g.gen.StartByPolicy(ctx, s.Line.ShotID, "", nil)
}

// report tells the user about the starts that did not happen and keeps them from being tried again by themselves: the budget ceiling goes
// to an Escalation (like the Retry Policy's limit), anything else to a notice. The Shot is left to the user. A start that was refused for a model
// that cannot be used (the preflight of the Generation Orchestrator) is reported with its reason like any other failed start, but once for each cause (the
// Shots refused for the same cause are released, not marked: refuseNoModel) and, for the same cause, not again by the looks after it (noModelTold).
func (g *Gate) report(ctx context.Context, p domain.Project, r round) error {
	for _, f := range r.failed {
		if err := g.markFailed(ctx, p, f); err != nil {
			return err
		}
		g.tell(ctx, p, fmt.Sprintf("%s を自動では始められませんでした: %s\nこの Shot は自動では進めません。Shot を選んで手動で起動してください。", f.step.name(), generation.FailureReason(f.err)))
	}
	for _, f := range r.noModel {
		if err := g.markFailed(ctx, p, f); err != nil {
			return err
		}
		g.tell(ctx, p, fmt.Sprintf("%s を自動では始められませんでした: %s\nこの Shot は自動では進めません。Shot を選んで手動で起動してください。\n"+
			"これは Shot ではなくモデルの状態が原因なので、同じ原因で始められない Shot は、失敗とは記録せずそのまま残しています。"+
			"API キーを登録するか割り当てを直したあと、チャットに何かメッセージを送ると、残りの Shot を続けます。同じ原因のお知らせは繰り返しません。",
			f.step.name(), generation.FailureReason(f.err)))
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
