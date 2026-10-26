package shotgen

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// since reports whether t was written after the job started (the database keeps milliseconds).
func since(t, start time.Time) bool { return !t.Before(start.Truncate(time.Millisecond)) }

// referencePackProbe: the Shot is GENERATING while the pack is built; once it is released the pack is what
// the Shot has now. (A failed run is told by the job table row, which carries the reason.)
func referencePackProbe(gen *service.GenerationService) mcpkit.Probe {
	return func(ctx context.Context, job mcpkit.JobInfo) (mcpkit.Progress, error) {
		frag, err := gen.Shot(ctx, job.Target)
		if err != nil {
			return mcpkit.Progress{}, err
		}
		p := mcpkit.Progress{Result: map[string]any{"shot_id": job.Target, "reference_count": len(frag.References())}}
		if frag.State == domain.ShotGenerating {
			p.State, p.Message = mcpkit.JobRunning, "Reference Pack を作っています"
		} else {
			p.State = mcpkit.JobSucceeded
		}
		return p, nil
	}
}

// judgeProbe: a result written after the job started means it succeeded; with none, the Shot is JUDGING while
// the run is queued or going, and a Shot that stopped without a new result means the run failed.
func judgeProbe(gen *service.GenerationService, traces *service.TraceService) mcpkit.Probe {
	return func(ctx context.Context, job mcpkit.JobInfo) (mcpkit.Progress, error) {
		tr, err := traces.Take(ctx, job.Target)
		if err != nil {
			return mcpkit.Progress{}, err
		}
		for _, res := range tr.Judgements { // newest first
			if since(res.CreatedAt, job.StartedAt) {
				return mcpkit.Progress{State: mcpkit.JobSucceeded, Done: 1, Total: 1, Result: map[string]any{
					"take_id": job.Target, "verdict": string(res.Result), "overall_confidence": res.OverallConfidence,
				}}, nil
			}
		}
		frag, err := gen.Shot(ctx, tr.Take.ShotID)
		if err != nil {
			return mcpkit.Progress{}, err
		}
		if frag.State == domain.ShotJudging {
			return mcpkit.Progress{State: mcpkit.JobRunning, Total: 1, Message: "AI Video Judge が判定しています"}, nil
		}
		return mcpkit.Progress{State: mcpkit.JobFailed, Total: 1, Error: "Judge の結果が作られないまま終わりました"}, nil
	}
}

// repairProbe: a repair record written after the job started with a Take it made means it succeeded. The
// Shot is REPAIRING while the work runs; a record without a new Take (the Retry Policy stopped, the
// regeneration failed) or no record at all is a failure, except Edit-Level Repair, which makes no Take.
func repairProbe(gen *service.GenerationService, traces *service.TraceService) mcpkit.Probe {
	return func(ctx context.Context, job mcpkit.JobInfo) (mcpkit.Progress, error) {
		tr, err := traces.Take(ctx, job.Target)
		if err != nil {
			return mcpkit.Progress{}, err
		}
		frag, err := gen.Shot(ctx, tr.Take.ShotID)
		if err != nil {
			return mcpkit.Progress{}, err
		}
		if frag.State == domain.ShotRepairing {
			return mcpkit.Progress{State: mcpkit.JobRunning, Total: 1, Message: "Repair しています"}, nil
		}
		for i := len(tr.Repairs) - 1; i >= 0; i-- { // newest first
			rec := tr.Repairs[i]
			if !since(rec.CreatedAt, job.StartedAt) {
				continue
			}
			result := map[string]any{"take_id": job.Target, "action": string(rec.Action), "new_take_id": rec.ResultingTakeID}
			if rec.ResultingTakeID == "" && rec.Action.Regenerates() {
				return mcpkit.Progress{State: mcpkit.JobFailed, Total: 1, Result: result, Error: "Repair は新しい Take を作れませんでした"}, nil
			}
			return mcpkit.Progress{State: mcpkit.JobSucceeded, Done: 1, Total: 1, Result: result}, nil
		}
		return mcpkit.Progress{State: mcpkit.JobFailed, Total: 1, Error: "Repair は Take を作りませんでした（Retry Policy の上限で、チャットに確認が出ている可能性があります）"}, nil
	}
}
