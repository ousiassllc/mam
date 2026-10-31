package judge

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/textutil"
)

// pick marks the best judged Take of the Shot as its AI Pick; in AUTO one the Retry Policy accepts (a
// pass reaching the Minimum acceptance scores) also becomes the Shot's Take, otherwise (DIRECTOR, Take
// Selection MANUAL / LOCK) it is only recommended. It returns the chat notice line ("" if unchanged).
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

// acceptance tells which Takes the Retry Policy accepts (the latest Judge result: verdict, LOCK
// violations, Minimum acceptance scores); nil (a pass is enough) when the policy cannot be read. A
// result is read when PickBest asks, inside its write transaction: a stale one let the newer Take win.
func (r *Runner) acceptance(ctx context.Context, ref repository.TakeRef) func(takeID string) bool {
	if r.Policies == nil {
		return nil
	}
	policy, err := r.Policies.Get(ctx, ref.ProjectID)
	if err != nil {
		r.Logger.Error("ai pick: read policy failed", "project_id", ref.ProjectID, "err", err)
		return nil
	}
	eff := policy.Effective()
	return func(takeID string) bool {
		res, err := r.Judges.Latest(ctx, takeID)
		return err == nil && res.Accepted(eff, policy.Template)
	}
}
