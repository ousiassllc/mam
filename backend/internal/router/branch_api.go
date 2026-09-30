package router

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// branchNodeBody is a Branch of the tree. The URLs are the ones the component uses: it never
// builds one itself (HATEOAS).
type branchNodeBody struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	ParentID        string           `json:"parent_id,omitempty" doc:"Branch the Branch Point lies in; empty when it lies in the trunk"`
	BranchPointType string           `json:"branch_point_type" enum:"scene,shot,frame"`
	SceneNumber     int              `json:"scene_number" doc:"1-based number of the Scene of the Branch Point in the series it lies in"`
	ShotNumber      int              `json:"shot_number,omitempty" doc:"1-based number of the Shot of the Branch Point; absent for a scene"`
	OffsetSeconds   float64          `json:"offset_seconds,omitempty" doc:"Position inside the Shot of a frame Branch Point"`
	SnapshotVersion int              `json:"snapshot_version" doc:"Number of the World State version (of the Branch's own series) the Branch refers to"`
	Version         int              `json:"version" doc:"Latest branch_version"`
	CreatedAt       time.Time        `json:"created_at"`
	DetailURL       string           `json:"detail_url"`
	WorldStateURL   string           `json:"world_state_url"`
	Children        []branchNodeBody `json:"children"`
}

type branchTreeBody struct {
	ProjectID string           `json:"project_id"`
	Branches  []branchNodeBody `json:"branches" doc:"Branches whose Branch Point lies in the trunk; the others are nested in children"`
}

func newBranchNode(project string, n *service.BranchNode) branchNodeBody {
	b := branchNodeBody{
		ID: n.ID, Name: n.Name, ParentID: n.ParentID, BranchPointType: string(n.PointType),
		SceneNumber: n.PointSceneNumber, ShotNumber: n.PointShotNumber, OffsetSeconds: n.PointOffsetSeconds,
		SnapshotVersion: n.WorldStateVersion, Version: n.Version, CreatedAt: n.CreatedAt,
		DetailURL:     "/branches/" + n.ID,
		WorldStateURL: "/projects/" + project + "/world-state?branch_id=" + n.ID,
		Children:      make([]branchNodeBody, len(n.Children)),
	}
	for i, c := range n.Children {
		b.Children[i] = newBranchNode(project, c)
	}
	return b
}

// registerBranchAPI registers GET /api/v1/projects/{id}/branches: the Branch tree (parents and
// children, the Snapshot each Branch refers to) for mam-branch-tree.
func registerBranchAPI(api huma.API, services *service.Services) {
	type output struct {
		Body branchTreeBody
	}
	huma.Register(api, huma.Operation{
		OperationID: "get-project-branches",
		Method:      "GET",
		Path:        APIPrefix + "/projects/{id}/branches",
		Summary:     "Branch tree of a Project (parent / child Branches and the Snapshot each refers to)",
		Tags:        []string{"branches"},
		Errors:      []int{404},
	}, func(ctx context.Context, in *struct {
		ID string `path:"id" doc:"Project ID"`
	}) (*output, error) {
		tree, err := services.Branch.Tree(ctx, in.ID)
		if err != nil {
			if isNotFound(err) {
				return nil, huma.Error404NotFound("Project が見つかりません")
			}
			return nil, err
		}
		body := branchTreeBody{ProjectID: tree.Project.ID, Branches: make([]branchNodeBody, len(tree.Roots))}
		for i, n := range tree.Roots {
			body.Branches[i] = newBranchNode(tree.Project.ID, n)
		}
		return &output{Body: body}, nil
	})
}
