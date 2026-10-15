package judge

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/textutil"
)

// pick marks the best judged Take of the Shot as its AI Pick: in AUTO a Take that the Retry Policy
// accepts (a pass that also reaches the Minimum acceptance scores, the rule the Repair loop applies)
// also becomes the Shot's Take, in DIRECTOR (or when the Shot's Take Selection is MANUAL / LOCK) the
// pick is only a recommendation. It returns the line
// of the chat notice about it ("" when the pick did not change or could not be made).
func (r *Runner) pick(ctx context.Context, ref repository.TakeRef) string {
	if r.Takes == nil || r.Projects == nil {
		return ""
	}
	ctx = context.WithoutCancel(ctx)
	project, err := r.Projects.Get(ctx, ref.ProjectID)
	if err != nil {
		r.Logger.Error("ai pick: read project failed", "project_id", ref.ProjectID, "err", err)
		return ""
	}
	// Take Selection MANUAL / LOCK (spec/spec.md §6): the AI keeps its hands off the Shot's Take, so
	// the pick is only a recommendation, as in DIRECTOR.
	adopt := project.Mode == domain.ModeAuto
	if adopt {
		locks, err := r.Generations.ShotLocks(ctx, ref.Take.ShotID)
		if err != nil {
			r.Logger.Error("ai pick: read shot locks failed", "shot_id", ref.Take.ShotID, "err", err)
			return ""
		}
		adopt = locks.Mode(domain.ParamTakeSelection) == domain.LockAuto
	}
	pick, err := r.Takes.PickBest(ctx, ref.Take.ShotID, adopt, r.acceptance(ctx, ref))
	if err != nil {
		r.Logger.Error("ai pick failed", "shot_id", ref.Take.ShotID, "err", err)
		return ""
	}
	if !pick.Changed {
		return ""
	}
	if pick.Adopted {
		return fmt.Sprintf("\nAI Pick: Take %s を採用しました。", textutil.ShortID(pick.TakeID))
	}
	return fmt.Sprintf("\nAI Pick: Take %s がおすすめです（Take 比較から選択できます）。", textutil.ShortID(pick.TakeID))
}

// acceptance tells which Takes of the Shot the Retry Policy of the Project accepts (the latest
// Judge result of the Take: verdict, LOCK violations and the Minimum acceptance scores). It returns
// nil, meaning a pass is enough, when the policy or the Takes cannot be read.
func (r *Runner) acceptance(ctx context.Context, ref repository.TakeRef) func(takeID string) bool {
	if r.Policies == nil {
		return nil
	}
	policy, err := r.Policies.Get(ctx, ref.ProjectID)
	if err != nil {
		r.Logger.Error("ai pick: read policy failed", "project_id", ref.ProjectID, "err", err)
		return nil
	}
	takes, err := r.Takes.ShotTakes(ctx, ref.Take.ShotID)
	if err != nil {
		r.Logger.Error("ai pick: read takes failed", "shot_id", ref.Take.ShotID, "err", err)
		return nil
	}
	eff := policy.Effective()
	accepted := make(map[string]bool, len(takes))
	for _, t := range takes {
		if res, err := r.Judges.Latest(ctx, t.ID); err == nil {
			accepted[t.ID] = res.Accepted(eff, policy.Template)
		}
	}
	return func(takeID string) bool { return accepted[takeID] }
}
