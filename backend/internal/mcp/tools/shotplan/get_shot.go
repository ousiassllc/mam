package shotplan

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// GetShotInput names the Shot.
type GetShotInput struct {
	ShotID string `json:"shot_id"`
}

// GetShotOutput is the Shot as GET /api/v1/shots/:id gives it.
type GetShotOutput struct {
	ID              string         `json:"id"`
	SceneID         string         `json:"scene_id"`
	ProjectID       string         `json:"project_id"`
	OrderIndex      int            `json:"order_index"`
	Objective       string         `json:"objective"`
	DurationSeconds float64        `json:"duration_seconds"`
	ShotSpec        map[string]any `json:"shot_spec" jsonschema:"Shot Spec（JSON オブジェクト）。無ければ {}"`
	State           string         `json:"state"`
	SelectedTakeID  string         `json:"selected_take_id,omitempty"`
}

func getShot(svc *service.GenerationService) mcpkit.Handler[GetShotInput, GetShotOutput] {
	return func(ctx context.Context, in GetShotInput) (GetShotOutput, error) {
		shot, err := svc.Shot(ctx, in.ShotID)
		if err != nil {
			return GetShotOutput{}, err
		}
		spec := map[string]any{}
		if len(shot.ShotSpec) > 0 {
			if err := json.Unmarshal(shot.ShotSpec, &spec); err != nil {
				return GetShotOutput{}, err
			}
		}
		return GetShotOutput{
			ID: shot.ID, SceneID: shot.SceneID, ProjectID: shot.ProjectID, OrderIndex: shot.OrderIndex, Objective: shot.Objective,
			DurationSeconds: shot.Duration, ShotSpec: spec, State: string(shot.State), SelectedTakeID: shot.SelectedTakeID,
		}, nil
	}
}
