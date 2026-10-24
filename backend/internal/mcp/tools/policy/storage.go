package policy

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// CostTotalsView is the cost of one part of a Project in USD.
type CostTotalsView struct {
	Estimated     float64 `json:"estimated" jsonschema:"生成時に記録した見積り"`
	Actual        float64 `json:"actual" jsonschema:"ベンダーが報告した実績の合計"`
	Entries       int     `json:"entries"`
	ActualEntries int     `json:"actual_entries" jsonschema:"実績のあるエントリー数。entries と同じなら実績は揃っている"`
}

func totalsOf(t domain.CostTotals) CostTotalsView {
	return CostTotalsView{Estimated: t.Estimated, Actual: t.Actual, Entries: t.Entries, ActualEntries: t.ActualEntries}
}

// CostSummaryOutput is the cost of a Project by Scene and Shot.
type CostSummaryOutput struct {
	Project           CostTotalsView  `json:"project"`
	Unassigned        CostTotalsView  `json:"unassigned" jsonschema:"どの Shot にも属さないコスト（削除された Shot など）"`
	ProjectCeilingUSD *float64        `json:"project_ceiling_usd"`
	ShotCeilingUSD    *float64        `json:"shot_ceiling_usd"`
	Scenes            []SceneCostView `json:"scenes"`
}

// SceneCostView is one Scene with its Shots.
type SceneCostView struct {
	SceneID    string         `json:"scene_id"`
	BranchID   string         `json:"branch_id,omitempty" jsonschema:"Branch の Scene のときだけ"`
	BranchName string         `json:"branch_name,omitempty"`
	OrderIndex int            `json:"order_index"`
	Title      string         `json:"title"`
	Totals     CostTotalsView `json:"totals"`
	Shots      []ShotCostView `json:"shots"`
}

// ShotCostView is one Shot.
type ShotCostView struct {
	ShotID     string         `json:"shot_id"`
	OrderIndex int            `json:"order_index"`
	Objective  string         `json:"objective"`
	Totals     CostTotalsView `json:"totals"`
}

func costOf(c domain.CostSummary) CostTotalsView { return totalsOf(c.Project) }

func (h handlers) cost(ctx context.Context, in ProjectInput) (CostSummaryOutput, error) {
	sum, err := h.svc.CostSummary(ctx, in.ProjectID)
	if err != nil {
		return CostSummaryOutput{}, err
	}
	out := CostSummaryOutput{
		Project: totalsOf(sum.Project), Unassigned: totalsOf(sum.Unassigned),
		ProjectCeilingUSD: sum.ProjectCeiling, ShotCeilingUSD: sum.ShotCeiling,
		Scenes: make([]SceneCostView, 0, len(sum.Scenes)),
	}
	for _, s := range sum.Scenes {
		scene := SceneCostView{
			SceneID: s.SceneID, BranchID: s.BranchID, BranchName: s.BranchName, OrderIndex: s.OrderIndex,
			Title: s.Title, Totals: totalsOf(s.Totals), Shots: make([]ShotCostView, 0, len(s.Shots)),
		}
		for _, sh := range s.Shots {
			scene.Shots = append(scene.Shots, ShotCostView{ShotID: sh.ShotID, OrderIndex: sh.OrderIndex, Objective: sh.Objective, Totals: totalsOf(sh.Totals)})
		}
		out.Scenes = append(out.Scenes, scene)
	}
	return out, nil
}

// StoredTakeView is a Take whose video file is stored.
type StoredTakeView struct {
	TakeID     string `json:"take_id"`
	ShotID     string `json:"shot_id"`
	SceneTitle string `json:"scene_title"`
	SceneIndex int    `json:"scene_index" jsonschema:"Scene の順番（0 始まり）"`
	ShotIndex  int    `json:"shot_index" jsonschema:"Scene の中の Shot の順番（0 始まり）"`
	Status     string `json:"status"`
	ModelName  string `json:"model_name"`
	SizeBytes  int64  `json:"size_bytes"`
	Unneeded   bool   `json:"unneeded" jsonschema:"アーカイブしても選択を失わない（archive_unneeded_takes の対象）"`
	Archivable bool   `json:"archivable" jsonschema:"archive_take で手放せる（選択中でなく、Timeline が使っていない）"`
}

// StorageOutput is the disk usage of a Project, the unneeded Takes first.
type StorageOutput struct {
	ProjectID     string           `json:"project_id"`
	Takes         []StoredTakeView `json:"takes"`
	AssetBytes    int64            `json:"asset_bytes" jsonschema:"Project が持つすべてのファイル（アップロード・参照画像・Take の動画）の大きさ"`
	PreviewBytes  int64            `json:"preview_bytes" jsonschema:"すべての Project が共有するプレビューのキャッシュの大きさ"`
	FreeBytes     uint64           `json:"free_bytes,omitempty" jsonschema:"アセットの置き場の空き容量。free_known が false のときは読めていない"`
	FreeKnown     bool             `json:"free_known"`
	UnneededCount int              `json:"unneeded_count"`
	UnneededBytes int64            `json:"unneeded_bytes"`
}

func storageOf(v service.StorageView) StorageOutput {
	count, bytes := v.UnneededTakes()
	out := StorageOutput{
		ProjectID: v.ProjectID, Takes: make([]StoredTakeView, 0, len(v.Takes)),
		AssetBytes: v.AssetBytes, PreviewBytes: v.PreviewBytes, FreeBytes: v.FreeBytes, FreeKnown: v.FreeKnown,
		UnneededCount: count, UnneededBytes: bytes,
	}
	for _, t := range v.Takes {
		out.Takes = append(out.Takes, StoredTakeView{
			TakeID: t.TakeID, ShotID: t.ShotID, SceneTitle: t.SceneTitle, SceneIndex: t.SceneIndex, ShotIndex: t.ShotIndex,
			Status: string(t.Status), ModelName: t.ModelName, SizeBytes: t.SizeBytes, Unneeded: t.Unneeded, Archivable: t.Archivable,
		})
	}
	return out
}

func (h handlers) storage(ctx context.Context, in ProjectInput) (StorageOutput, error) {
	v, err := h.svc.Storage(ctx, in.ProjectID)
	return storageOf(v), err
}

// ArchiveTakeInput names the Take.
type ArchiveTakeInput struct {
	ProjectID string `json:"project_id"`
	TakeID    string `json:"take_id" jsonschema:"get_storage_usage が返す take_id"`
}

func (h handlers) archiveTake(ctx context.Context, in ArchiveTakeInput) (StorageOutput, error) {
	v, err := h.svc.ArchiveTake(ctx, in.ProjectID, in.TakeID)
	return storageOf(v), err
}

func (h handlers) archiveUnneeded(ctx context.Context, in ProjectInput) (StorageOutput, error) {
	v, err := h.svc.ArchiveUnneeded(ctx, in.ProjectID)
	return storageOf(v), err
}
