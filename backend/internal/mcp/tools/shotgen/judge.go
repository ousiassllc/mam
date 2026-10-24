package shotgen

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// TakeInput names a Take.
type TakeInput struct {
	TakeID string `json:"take_id"`
}

// TakeJobOutput is the job of an operation on a Take.
type TakeJobOutput struct {
	mcpkit.JobRef
	TakeID    string `json:"take_id"`
	ShotID    string `json:"shot_id"`
	ShotState string `json:"shot_state"`
}

type takeAction func(ctx context.Context, takeID string) (service.ShotFragment, error)

func takeJob(r *mcpkit.Registrar, kind string, act takeAction) mcpkit.Handler[TakeInput, TakeJobOutput] {
	return func(ctx context.Context, in TakeInput) (TakeJobOutput, error) {
		var frag service.ShotFragment
		ref, err := r.Jobs.Run(ctx, kind, in.TakeID, func(ctx context.Context) (string, error) {
			var err error
			frag, err = act(ctx, in.TakeID)
			return frag.JobID, err
		})
		if err != nil {
			return TakeJobOutput{}, err
		}
		return TakeJobOutput{JobRef: ref, TakeID: in.TakeID, ShotID: frag.ID, ShotState: string(frag.State)}, nil
	}
}

func judge(r *mcpkit.Registrar, svc *service.JudgeService) mcpkit.Handler[TakeInput, TakeJobOutput] {
	return takeJob(r, KindJudgeTake, svc.Judge)
}

func repair(r *mcpkit.Registrar, svc *service.RepairService) mcpkit.Handler[TakeInput, TakeJobOutput] {
	return takeJob(r, KindRepairTake, svc.Repair)
}

// JudgeResultOutput is the latest Judge result of a Take.
type JudgeResultOutput struct {
	TakeID string             `json:"take_id"`
	Result domain.JudgeResult `json:"judge_result"`
}

func getJudgeResult(svc *service.JudgeService) mcpkit.Handler[TakeInput, JudgeResultOutput] {
	return func(ctx context.Context, in TakeInput) (JudgeResultOutput, error) {
		res, err := svc.LatestResult(ctx, in.TakeID)
		if err != nil {
			return JudgeResultOutput{}, err
		}
		return JudgeResultOutput{TakeID: in.TakeID, Result: res}, nil
	}
}

// ShadowReportOutput is the report of the Jev shadow runs.
type ShadowReportOutput struct {
	Report domain.ShadowReport `json:"report"`
}

// ShadowReportInput has no field.
type ShadowReportInput struct{}

func getShadowReport(svc *service.RepairService) mcpkit.Handler[ShadowReportInput, ShadowReportOutput] {
	return func(ctx context.Context, _ ShadowReportInput) (ShadowReportOutput, error) {
		rep, err := svc.ShadowReport(ctx)
		if err != nil {
			return ShadowReportOutput{}, err
		}
		return ShadowReportOutput{Report: rep}, nil
	}
}
