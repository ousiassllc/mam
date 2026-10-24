// Package shotplan holds the MCP tools of the Scene / Shot hierarchy (the Scene page): reading it and adding
// a Scene or a Shot.
package shotplan

import (
	"context"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotview"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Read(r, "get_scene_hierarchy",
		"Scene / Shot / Take の階層（Scene ページと同じ内容）。scene_id 省略で最初の Scene / The Scene / Shot / Take hierarchy of a Project.",
		getHierarchy(svc.Plans))
	mcpkit.Write(r, "create_scene",
		"Project の末尾に Scene を追加する / Add a Scene to a Project.", createScene(svc.Plans))
	mcpkit.Write(r, "create_shot",
		"Scene の末尾に Shot を追加する / Add a Shot to a Scene.", createShot(svc.Plans))
	mcpkit.Read(r, "get_shot",
		"Shot の基本情報・shot_spec・採用 Take（GET /api/v1/shots/:id と同じ）/ A Shot with its shot_spec, duration and selected Take.",
		getShot(svc.Generation))
}

// SceneBrief is one Scene.
type SceneBrief struct {
	ID         string `json:"id"`
	OrderIndex int    `json:"order_index"`
	Title      string `json:"title"`
	Location   string `json:"location,omitempty"`
	Time       string `json:"time,omitempty"`
	Objective  string `json:"objective,omitempty"`
	State      string `json:"state"`
	ShotCount  int    `json:"shot_count"`
}

func sceneBrief(s domain.Scene, shots int) SceneBrief {
	return SceneBrief{ID: s.ID, OrderIndex: s.OrderIndex, Title: s.Title, Location: s.Location, Time: s.Time, Objective: s.Objective, State: string(s.State), ShotCount: shots}
}

// HierarchyInput selects the Scene.
type HierarchyInput struct {
	ProjectID string `json:"project_id"`
	SceneID   string `json:"scene_id,omitempty" jsonschema:"表示する Scene（省略で最初の Scene）"`
}

// HierarchyOutput is the hierarchy: every Scene, and the Shots (with Takes) of the selected one.
type HierarchyOutput struct {
	ProjectID string          `json:"project_id"`
	Scenes    []SceneBrief    `json:"scenes"`
	Scene     *SceneBrief     `json:"scene,omitempty" jsonschema:"選択された Scene。Scene が無い Project では省略"`
	Shots     []shotview.Card `json:"shots"`
}

func hierarchyOutput(v service.SceneView) HierarchyOutput {
	out := HierarchyOutput{ProjectID: v.Project.ID, Scenes: make([]SceneBrief, 0, len(v.Scenes)), Shots: make([]shotview.Card, 0, len(v.Shots))}
	for _, s := range v.Scenes {
		b := sceneBrief(s.Scene, s.ShotCount)
		out.Scenes = append(out.Scenes, b)
		if v.Scene != nil && v.Scene.ID == s.ID {
			out.Scene = &b
		}
	}
	for _, sh := range v.Shots {
		out.Shots = append(out.Shots, shotview.FromView(v.Project.ID, sh))
	}
	return out
}

func getHierarchy(svc *service.PlanService) mcpkit.Handler[HierarchyInput, HierarchyOutput] {
	return func(ctx context.Context, in HierarchyInput) (HierarchyOutput, error) {
		view, err := svc.Hierarchy(ctx, in.ProjectID, in.SceneID, "")
		if err != nil {
			return HierarchyOutput{}, err
		}
		return hierarchyOutput(view), nil
	}
}

// CreateSceneInput is the Scene form.
type CreateSceneInput struct {
	ProjectID string `json:"project_id"`
	Title     string `json:"title" jsonschema:"Scene の見出し（必須）"`
	Location  string `json:"location,omitempty"`
	Time      string `json:"time,omitempty" jsonschema:"時間帯など"`
	Objective string `json:"objective,omitempty"`
}

// CreateSceneOutput is the new Scene.
type CreateSceneOutput struct {
	Scene SceneBrief `json:"scene"`
}

func createScene(svc *service.PlanService) mcpkit.Handler[CreateSceneInput, CreateSceneOutput] {
	return func(ctx context.Context, in CreateSceneInput) (CreateSceneOutput, error) {
		view, err := svc.CreateScene(ctx, in.ProjectID, service.SceneForm{Title: in.Title, Location: in.Location, Time: in.Time, Objective: in.Objective})
		if err != nil {
			return CreateSceneOutput{}, err
		}
		return CreateSceneOutput{Scene: sceneBrief(*view.Scene, len(view.Shots))}, nil
	}
}

// CreateShotInput is the Shot form.
type CreateShotInput struct {
	SceneID         string  `json:"scene_id"`
	Objective       string  `json:"objective" jsonschema:"Shot の狙い（必須）"`
	DurationSeconds float64 `json:"duration_seconds" jsonschema:"秒数（0 より大きく、上限あり）"`
}

// CreateShotOutput is the new Shot.
type CreateShotOutput struct {
	Shot shotview.Card `json:"shot"`
}

func createShot(svc *service.PlanService) mcpkit.Handler[CreateShotInput, CreateShotOutput] {
	return func(ctx context.Context, in CreateShotInput) (CreateShotOutput, error) {
		form := service.ShotForm{Objective: in.Objective, DurationSeconds: strconv.FormatFloat(in.DurationSeconds, 'f', -1, 64)}
		view, err := svc.CreateShot(ctx, in.SceneID, form)
		if err != nil {
			return CreateShotOutput{}, err
		}
		for _, sh := range view.Shots {
			if sh.Selected { // CreateShot selects the new Shot
				return CreateShotOutput{Shot: shotview.FromView(view.Project.ID, sh)}, nil
			}
		}
		return CreateShotOutput{}, service.ErrNotFound
	}
}
