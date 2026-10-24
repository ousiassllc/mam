package director

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// ShotInput names the Shot.
type ShotInput struct {
	ShotID string `json:"shot_id"`
}

// ShotBrief is the Shot of the Inspector.
type ShotBrief struct {
	ID              string  `json:"id"`
	SceneID         string  `json:"scene_id"`
	OrderIndex      int     `json:"order_index"`
	Objective       string  `json:"objective"`
	DurationSeconds float64 `json:"duration_seconds"`
	State           string  `json:"state"`
	SelectedTakeID  string  `json:"selected_take_id,omitempty"`
}

// JudgeBrief is the latest Judge result of a Take (get_take_judge_result has all of it).
type JudgeBrief struct {
	Verdict           string    `json:"verdict" jsonschema:"pass or fail"`
	OverallConfidence float64   `json:"overall_confidence"`
	JudgedAt          time.Time `json:"judged_at"`
}

// InspectorTake is a Take of the Inspector.
type InspectorTake struct {
	ID        string      `json:"id"`
	Status    string      `json:"status"`
	ModelName string      `json:"model_name"`
	Adopted   bool        `json:"adopted"`
	HasVideo  bool        `json:"has_video"`
	CreatedAt time.Time   `json:"created_at"`
	Judge     *JudgeBrief `json:"judge,omitempty" jsonschema:"Judge 済みなら最新の結果"`
}

// LibraryItem is an image of the Reference Library that can be added to the Reference Pack.
type LibraryItem struct {
	ID           string   `json:"id"`
	AssetID      string   `json:"asset_id"`
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Tags         []string `json:"tags"`
	Source       string   `json:"source"`
	SourceShotID string   `json:"source_shot_id,omitempty"`
}

// Version is a saved version of the Shot Spec.
type Version struct {
	Number    int       `json:"version_number"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

// InspectorOutput is the Shot Inspector.
type InspectorOutput struct {
	ProjectID     string             `json:"project_id"`
	ProjectMode   string             `json:"project_mode" jsonschema:"auto or director"`
	SceneID       string             `json:"scene_id"`
	SceneTitle    string             `json:"scene_title"`
	Shot          ShotBrief          `json:"shot"`
	Spec          map[string]any     `json:"spec,omitempty" jsonschema:"Canonical Shot Spec。まだ無ければ省略"`
	Locks         map[string]Lock    `json:"locks" jsonschema:"パラメータ名 → AUTO/MANUAL/LOCK 設定"`
	Takes         []InspectorTake    `json:"takes"`
	ReferencePack []domain.Reference `json:"reference_pack"`
	Library       []LibraryItem      `json:"library"`
	Versions      []Version          `json:"versions" jsonschema:"Shot Spec の保存済みバージョン（新しい順）"`
}

func inspectorOutput(v service.Inspector) (InspectorOutput, error) {
	out := InspectorOutput{
		ProjectID: v.Project.ID, ProjectMode: string(v.Project.Mode), SceneID: v.Scene.ID, SceneTitle: v.Scene.Title,
		Shot: ShotBrief{
			ID: v.Shot.ID, SceneID: v.Shot.SceneID, OrderIndex: v.Shot.OrderIndex, Objective: v.Shot.Objective,
			DurationSeconds: v.Shot.Duration, State: string(v.Shot.State), SelectedTakeID: v.Shot.SelectedTakeID,
		},
		Locks:         locksOut(v.Locks, domain.ShotLockParameters()),
		Takes:         make([]InspectorTake, 0, len(v.Takes)),
		ReferencePack: append([]domain.Reference{}, v.Pack...),
		Library:       make([]LibraryItem, 0, len(v.Library)),
		Versions:      make([]Version, 0, len(v.Versions)),
	}
	if v.Spec != nil {
		raw, err := json.Marshal(v.Spec)
		if err != nil {
			return InspectorOutput{}, err
		}
		if err := json.Unmarshal(raw, &out.Spec); err != nil {
			return InspectorOutput{}, err
		}
	}
	for _, t := range v.Takes {
		it := InspectorTake{ID: t.ID, Status: string(t.Status), ModelName: t.ModelName, Adopted: t.ID == v.Shot.SelectedTakeID, HasVideo: t.FilePath != "", CreatedAt: t.CreatedAt}
		if j, ok := v.Judged[t.ID]; ok {
			it.Judge = &JudgeBrief{Verdict: string(j.Result), OverallConfidence: j.OverallConfidence, JudgedAt: j.CreatedAt}
		}
		out.Takes = append(out.Takes, it)
	}
	for _, l := range v.Library {
		tags := l.Tags
		if tags == nil {
			tags = []string{}
		}
		out.Library = append(out.Library, LibraryItem{ID: l.ID, AssetID: l.AssetID, Kind: string(l.Kind), Name: l.Name, Tags: tags, Source: string(l.Source), SourceShotID: l.SourceShotID})
	}
	for _, e := range v.Versions {
		out.Versions = append(out.Versions, Version{Number: e.Number, Summary: e.Summary, CreatedAt: e.CreatedAt})
	}
	return out, nil
}

func getInspector(svc *service.DirectorService) mcpkit.Handler[ShotInput, InspectorOutput] {
	return func(ctx context.Context, in ShotInput) (InspectorOutput, error) {
		view, err := svc.Inspector(ctx, in.ShotID)
		if err != nil {
			return InspectorOutput{}, err
		}
		return inspectorOutput(view)
	}
}
