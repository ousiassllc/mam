package branches

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// KindCreateBranch is the job kind of create_branch with a Direction: making the Branch, planning its first
// Shot and generating it. The target is the Shot the Branch was made at.
const KindCreateBranch = "create_branch"

// The start of the job (BranchService.Create) is synchronous: it has planned the first Shot and queued its
// generation by the time Jobs.Run returns, so a Branch without a Shot means that step failed.
//
// probe finds the Branch the job made (the oldest Branch of the Project created since the job started) and
// reads its first Shot like a Shot generation: running while the Shot is being made, succeeded when it
// has a Take made since the job started.
func probe(branches *service.BranchService, gen *service.GenerationService) mcpkit.Probe {
	return func(ctx context.Context, job mcpkit.JobInfo) (mcpkit.Progress, error) {
		source, err := gen.Shot(ctx, job.Target)
		if err != nil {
			return mcpkit.Progress{}, err
		}
		since := job.StartedAt.Truncate(time.Millisecond)
		tree, err := branches.Tree(ctx, source.ProjectID)
		if err != nil {
			return mcpkit.Progress{}, err
		}
		var made *domain.Branch
		for i, b := range tree.Branches {
			if !b.CreatedAt.Before(since) && (made == nil || b.CreatedAt.Before(made.CreatedAt)) {
				made = &tree.Branches[i]
			}
		}
		if made == nil {
			return mcpkit.Progress{State: mcpkit.JobRunning, Message: "Branch を作っています"}, nil
		}
		d, err := branches.Detail(ctx, made.ID)
		if err != nil {
			return mcpkit.Progress{}, err
		}
		result := map[string]any{"branch_id": made.ID}
		if len(d.Shots) == 0 {
			return mcpkit.Progress{State: mcpkit.JobFailed, Error: "Branch の最初の Shot が作られませんでした", Result: result}, nil
		}
		first := d.Shots[0].Shot.ID
		frag, err := gen.Shot(ctx, first)
		if err != nil {
			return mcpkit.Progress{}, err
		}
		fresh := []string{}
		for _, t := range frag.Takes {
			if !t.CreatedAt.Before(since) {
				fresh = append(fresh, t.ID)
			}
		}
		result["shot_id"], result["shot_state"], result["take_ids"] = first, string(frag.State), fresh
		p := mcpkit.Progress{Done: len(fresh), Result: result}
		switch frag.State {
		case domain.ShotGenerating, domain.ShotJudging, domain.ShotRepairing:
			p.State, p.Message = mcpkit.JobRunning, "Branch の最初の Shot を生成しています"
		default:
			if len(fresh) > 0 {
				p.State = mcpkit.JobSucceeded
			} else {
				p.State, p.Error = mcpkit.JobFailed, "新しい Take が作られないまま終わりました"
			}
		}
		return p, nil
	}
}
