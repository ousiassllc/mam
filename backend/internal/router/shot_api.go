package router

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// shotBody is a Shot for the components: its spec, planned length and the Take it uses.
type shotBody struct {
	ID              string          `json:"id"`
	SceneID         string          `json:"scene_id"`
	ProjectID       string          `json:"project_id"`
	OrderIndex      int             `json:"order_index"`
	Objective       string          `json:"objective"`
	DurationSeconds float64         `json:"duration_seconds" doc:"Length planned for the Shot"`
	ShotSpec        json.RawMessage `json:"shot_spec" doc:"Shot Spec (JSON object); {} when the Shot has none"`
	State           string          `json:"state"`
	SelectedTakeID  string          `json:"selected_take_id,omitempty" doc:"Take the Shot uses; omitted until one is selected"`
}

// registerShotAPI registers GET /api/v1/shots/{id}: the Shot itself, for mam-video-player and
// mam-timeline-editor.
func registerShotAPI(api huma.API, services *service.Services) {
	type output struct {
		Body shotBody
	}
	huma.Register(api, huma.Operation{
		OperationID: "get-shot",
		Method:      "GET",
		Path:        APIPrefix + "/shots/{id}",
		Summary:     "A Shot with its shot_spec, duration and selected Take",
		Tags:        []string{"shots"},
		Errors:      []int{404},
	}, func(ctx context.Context, in *struct {
		ID string `path:"id" doc:"Shot ID"`
	}) (*output, error) {
		shot, err := services.Generation.Shot(ctx, in.ID)
		if err != nil {
			if isNotFound(err) {
				return nil, huma.Error404NotFound("Shot が見つかりません")
			}
			return nil, err
		}
		spec := shot.ShotSpec
		if len(spec) == 0 {
			spec = json.RawMessage("{}")
		}
		return &output{Body: shotBody{
			ID: shot.ID, SceneID: shot.SceneID, ProjectID: shot.ProjectID, OrderIndex: shot.OrderIndex,
			Objective: shot.Objective, DurationSeconds: shot.Duration, ShotSpec: spec,
			State: string(shot.State), SelectedTakeID: shot.SelectedTakeID,
		}}, nil
	})
}
