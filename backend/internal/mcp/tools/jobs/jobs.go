// Package jobs holds get_job, the status tool of every long operation, list_jobs, which searches their
// history, and the Probe of the generation job (generate_shot).
package jobs

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// KindGenerateShot is the job kind of a Shot generation (generation, Prompt Candidates).
const KindGenerateShot = "generate_shot"

// Register adds get_job, list_jobs and the Probe of KindGenerateShot.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	r.Jobs.RegisterKind(KindGenerateShot, shotProbe(svc.Generation))
	mcpkit.Read(r, "get_job",
		"長い操作（生成など）の状態・進捗・結果・失敗理由を返す / Status, progress, result and failure reason of a long operation. job_id は開始したツールが返したもの。",
		func(ctx context.Context, in Input) (mcpkit.JobStatus, error) { return r.Jobs.Get(ctx, in.JobID) })
	mcpkit.Read(r, "list_jobs", listDescription, func(ctx context.Context, in ListInput) (ListOutput, error) { return list(ctx, r.Jobs, in) })
}

// Input names the job.
type Input struct {
	JobID string `json:"job_id" jsonschema:"開始したツールが返した job_id（ジョブキューの ID も可）"`
}

// shotProbe reads a Shot generation from the Shot: it is running while the Shot is GENERATING, JUDGING or
// REPAIRING, and it succeeded when the Shot has a Take made since the job started. A Shot that stopped
// without a new Take means the generation failed (the job table row, when the job has one, carries the reason).
func shotProbe(gen *service.GenerationService) mcpkit.Probe {
	return func(ctx context.Context, job mcpkit.JobInfo) (mcpkit.Progress, error) {
		frag, err := gen.Shot(ctx, job.Target)
		if err != nil {
			return mcpkit.Progress{}, err
		}
		fresh := []string{}
		for _, t := range frag.Takes {
			if !t.CreatedAt.Before(job.StartedAt.Truncate(time.Millisecond)) {
				fresh = append(fresh, t.ID)
			}
		}
		p := mcpkit.Progress{Done: len(fresh), Result: map[string]any{"shot_id": job.Target, "shot_state": string(frag.State), "take_ids": fresh}}
		switch frag.State {
		case domain.ShotGenerating:
			p.State, p.Message = mcpkit.JobRunning, "Take を生成しています"
		case domain.ShotJudging:
			p.State, p.Message = mcpkit.JobRunning, "AI Video Judge が判定しています"
		case domain.ShotRepairing:
			p.State, p.Message = mcpkit.JobRunning, "Repair しています"
		case domain.ShotPlanning, domain.ShotEditing:
			if len(fresh) > 0 {
				p.State = mcpkit.JobSucceeded
			} else {
				p.State, p.Error = mcpkit.JobFailed, "新しい Take が作られないまま終わりました"
			}
		}
		return p, nil
	}
}
