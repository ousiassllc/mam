package branches

import (
	"context"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func registerWrites(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Write(r, "create_branch",
		"Shot に分岐点を作って Branch を作り、その時点の World State を保存する。direction を渡すと最初の Shot の計画と生成も始め、job_id を返す（get_job）/ Make a Branch at a Shot. With a direction the first Shot is planned and generated (job_id).",
		createBranch(r, svc.Branch), mcpkit.OpenWorld())
	mcpkit.Write(r, "update_branch",
		"Branch の名前の変更、または参照する World State のバージョンの付け替え（どちらも履歴に残る）/ Rename a Branch and/or point it at another World State version of its own series.",
		updateBranch(svc.Branch))
}

// CreateInput is the form of the Branch Point.
type CreateInput struct {
	ShotID        string  `json:"shot_id" jsonschema:"分岐点の Shot（get_branch_tree の shots）"`
	PointType     string  `json:"point_type,omitempty" jsonschema:"scene（Shot の Scene の頭）, shot（Shot の後）, frame（Shot の中の位置）。省略すると shot"`
	OffsetSeconds *string `json:"offset_seconds,omitempty" jsonschema:"frame のときの位置（秒）。0 以上 Shot の長さ以内"`
	Name          string  `json:"name,omitempty" jsonschema:"Branch の名前。省略すると direction か「Branch N」"`
	Direction     string  `json:"direction,omitempty" jsonschema:"別展開の指示。渡すと AI が最初の Shot を計画して生成する（生成の設定が必要）"`
}

// CreateOutput is the new Branch (detail) and, when a Direction started the first Shot, its job.
type CreateOutput struct {
	DetailOutput
	Job *mcpkit.JobRef `json:"job,omitempty" jsonschema:"direction を渡したときの、最初の Shot の生成のジョブ。get_job で進捗を取る"`
}

func createBranch(r *mcpkit.Registrar, svc *service.BranchService) mcpkit.Handler[CreateInput, CreateOutput] {
	return func(ctx context.Context, in CreateInput) (CreateOutput, error) {
		offset := ""
		if in.OffsetSeconds != nil {
			offset = *in.OffsetSeconds
		}
		form := service.BranchInput{PointType: domain.BranchPointType(in.PointType), OffsetSeconds: offset, Name: in.Name, Direction: in.Direction}
		if in.Direction == "" {
			d, err := svc.Create(ctx, in.ShotID, form)
			return CreateOutput{DetailOutput: detailOf(d)}, err
		}
		var d service.BranchDetail
		ref, err := r.Jobs.Run(ctx, KindCreateBranch, in.ShotID, func(ctx context.Context) (string, error) {
			var err error
			d, err = svc.Create(ctx, in.ShotID, form)
			return "", err
		})
		if err != nil {
			return CreateOutput{}, err
		}
		return CreateOutput{DetailOutput: detailOf(d), Job: &ref}, nil
	}
}

// UpdateInput names the Branch and what to change; neither given is a validation error.
type UpdateInput struct {
	BranchID          string  `json:"branch_id"`
	Name              *string `json:"name,omitempty" jsonschema:"新しい名前（100 文字以内）"`
	WorldStateVersion *int    `json:"world_state_version,omitempty" jsonschema:"参照する World State のバージョン（1 から get_branch の latest_world_version まで）"`
}

func updateBranch(svc *service.BranchService) mcpkit.Handler[UpdateInput, DetailOutput] {
	return func(ctx context.Context, in UpdateInput) (DetailOutput, error) {
		var version *string
		if in.WorldStateVersion != nil {
			v := strconv.Itoa(*in.WorldStateVersion)
			version = &v
		}
		d, err := svc.Update(ctx, in.BranchID, in.Name, version)
		if err != nil {
			return DetailOutput{}, err
		}
		return detailOf(d), nil
	}
}
