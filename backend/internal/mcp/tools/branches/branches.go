// Package branches holds the MCP tools of the Branch screens (/projects/:id/branches, /branches/:id): the
// Branch tree, the detail of a Branch, creating a Branch at a Shot (with a Direction it also starts the
// first Shot of the Branch: a job) and renaming a Branch or moving its World State reference.
package branches

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldcommon"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package and the Probe of KindCreateBranch.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	r.Jobs.RegisterKind(KindCreateBranch, probe(svc.Branch, svc.Generation))
	mcpkit.Read(r, "get_branch_tree",
		"Project の Branch 一覧（parent_id で木になる）と、Branch を作れる Shot の一覧 / The Branches of a Project (a tree through parent_id) and the Shots a Branch can start from.",
		tree(svc.Branch))
	mcpkit.Read(r, "get_branch",
		"Branch の詳細: 分岐点・親・Branch 自身の Scene と Shot・バージョン・参照できる World State のバージョン数 / One Branch with its own Scenes, Shots and versions.",
		detail(svc.Branch))
	registerWrites(r, svc)
}

// Branch is a row of the Branch tree.
type Branch struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"project_id"`
	ParentID           string    `json:"parent_id,omitempty" jsonschema:"分岐点がある Branch。本線から分岐したものは空"`
	Name               string    `json:"name"`
	PointType          string    `json:"point_type" jsonschema:"scene, shot, frame"`
	PointText          string    `json:"point_text" jsonschema:"画面と同じ分岐点の表記（Scene 2 / Shot 1 の後 など）"`
	PointSceneID       string    `json:"point_scene_id,omitempty"`
	PointShotID        string    `json:"point_shot_id,omitempty"`
	PointOffsetSeconds float64   `json:"point_offset_seconds,omitempty"`
	WorldStateVersion  int       `json:"world_state_version" jsonschema:"この Branch が参照する World State のバージョン（Branch 自身の系列の番号）"`
	Version            int       `json:"version" jsonschema:"Branch 自身の最新バージョン番号"`
	CreatedAt          time.Time `json:"created_at"`
}

func branchOf(b domain.Branch) Branch {
	return Branch{
		ID: b.ID, ProjectID: b.ProjectID, ParentID: b.ParentID, Name: b.Name, PointType: string(b.PointType), PointText: b.PointText(),
		PointSceneID: b.PointSceneID, PointShotID: b.PointShotID, PointOffsetSeconds: b.PointOffsetSeconds,
		WorldStateVersion: b.WorldStateVersion, Version: b.Version, CreatedAt: b.CreatedAt,
	}
}

// TreeInput names the Project.
type TreeInput struct {
	ProjectID string `json:"project_id"`
}

// ShotOption is a Shot a Branch can be made at.
type ShotOption struct {
	ShotID string `json:"shot_id"`
	Label  string `json:"label" jsonschema:"Scene 2 / Shot 1。Branch の Shot は Branch 名が前に付く"`
}

// TreeOutput is the Branch tree page.
type TreeOutput struct {
	ProjectID string       `json:"project_id"`
	Branches  []Branch     `json:"branches" jsonschema:"作成順。parent_id でたどると木になる"`
	Shots     []ShotOption `json:"shots" jsonschema:"create_branch の shot_id に渡せる Shot"`
}

func tree(svc *service.BranchService) mcpkit.Handler[TreeInput, TreeOutput] {
	return func(ctx context.Context, in TreeInput) (TreeOutput, error) {
		t, err := svc.Tree(ctx, in.ProjectID)
		if err != nil {
			return TreeOutput{}, err
		}
		out := TreeOutput{ProjectID: t.Project.ID, Branches: make([]Branch, 0, len(t.Branches)), Shots: make([]ShotOption, 0, len(t.Shots))}
		for _, b := range t.Branches {
			out.Branches = append(out.Branches, branchOf(b))
		}
		for _, s := range t.Shots {
			out.Shots = append(out.Shots, ShotOption{ShotID: s.ShotID, Label: s.Label})
		}
		return out, nil
	}
}

// DetailInput names the Branch.
type DetailInput struct {
	BranchID string `json:"branch_id"`
}

// Scene is a Scene of the Branch's own tree.
type Scene struct {
	ID        string `json:"id"`
	Number    int    `json:"number" jsonschema:"1 始まりの番号"`
	Title     string `json:"title"`
	Location  string `json:"location,omitempty"`
	Time      string `json:"time,omitempty"`
	Objective string `json:"objective,omitempty"`
	State     string `json:"state"`
}

// Shot is a Shot of the Branch's own tree.
type Shot struct {
	ID        string  `json:"id"`
	Scene     int     `json:"scene" jsonschema:"Scene の番号（1 始まり）"`
	Number    int     `json:"number" jsonschema:"Scene 内の Shot の番号（1 始まり）"`
	Objective string  `json:"objective,omitempty"`
	Duration  float64 `json:"duration_seconds,omitempty"`
	State     string  `json:"state"`
}

// DetailOutput is the Branch page.
type DetailOutput struct {
	Branch             Branch                     `json:"branch"`
	Parent             *Branch                    `json:"parent,omitempty"`
	Scenes             []Scene                    `json:"scenes"`
	Shots              []Shot                     `json:"shots"`
	Versions           []worldcommon.VersionEntry `json:"versions" jsonschema:"Branch 自身のバージョン（新しい順）。restore_branch_version で戻せる"`
	LatestWorldVersion int                        `json:"latest_world_version" jsonschema:"update_branch の world_state_version に指定できるのは 1 からこの番号まで"`
	Started            bool                       `json:"started" jsonschema:"最初の Shot の生成を始めたときだけ true（create_branch の応答）"`
}

func detailOf(d service.BranchDetail) DetailOutput {
	out := DetailOutput{
		Branch: branchOf(d.Branch), Scenes: make([]Scene, 0, len(d.Scenes)), Shots: make([]Shot, 0, len(d.Shots)),
		Versions: worldcommon.VersionEntries(branchVersions(d.Versions)), LatestWorldVersion: d.LatestWorldVersion, Started: d.Started,
	}
	if d.Parent != nil {
		p := branchOf(*d.Parent)
		out.Parent = &p
	}
	for _, s := range d.Scenes {
		out.Scenes = append(out.Scenes, Scene{
			ID: s.ID, Number: s.OrderIndex + 1, Title: s.Title, Location: s.Location, Time: s.Time, Objective: s.Objective, State: string(s.State),
		})
	}
	for _, s := range d.Shots {
		out.Shots = append(out.Shots, Shot{
			ID: s.Shot.ID, Scene: s.Scene, Number: s.Shot.OrderIndex + 1, Objective: s.Shot.Objective, Duration: s.Shot.Duration, State: string(s.Shot.State),
		})
	}
	return out
}

func branchVersions(versions []domain.BranchVersion) []domain.VersionEntry {
	out := make([]domain.VersionEntry, 0, len(versions))
	for _, v := range versions {
		out = append(out, domain.VersionEntry{Number: v.Number, Summary: v.Name, CreatedAt: v.CreatedAt})
	}
	return out
}

func detail(svc *service.BranchService) mcpkit.Handler[DetailInput, DetailOutput] {
	return func(ctx context.Context, in DetailInput) (DetailOutput, error) {
		d, err := svc.Detail(ctx, in.BranchID)
		if err != nil {
			return DetailOutput{}, err
		}
		return detailOf(d), nil
	}
}
