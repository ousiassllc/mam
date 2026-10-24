// Package worldstate holds the MCP tools of the World State screen (/projects/:id/world-state): the view,
// the edit of a Character, Product or Location, the field LOCKs and the reference assignment.
package worldstate

import (
	"context"
	"sort"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldcommon"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	world := svc.World
	mcpkit.Read(r, "get_world_state",
		"World State（登場人物・商品・場所・カメラ・ストーリー、LOCK 中の項目、Reference Library、保存済みバージョン）を返す。branch_id を渡すとその Branch の World State / The current World State with LOCKs, Reference Library and versions. 画面の World State ページと同じ。",
		getWorldState(world))
	registerEdits(r, world)
	registerLocks(r, world)
}

// Scope names the World State: the trunk of a Project, or one of its Branches.
type Scope struct {
	ProjectID string `json:"project_id"`
	BranchID  string `json:"branch_id,omitempty" jsonschema:"Branch の World State を扱うときの Branch ID。省略すると本線"`
}

// FieldLock is one attribute of the World State that is LOCKed.
type FieldLock struct {
	TargetType string `json:"target_type" jsonschema:"character, product, location, camera, story"`
	TargetID   string `json:"target_id,omitempty" jsonschema:"character / product / location の ID。camera と story は空"`
	Parameter  string `json:"parameter"`
}

// ProjectLock is the AUTO / MANUAL / LOCK mode of Story or Music of the Project.
type ProjectLock struct {
	Parameter string `json:"parameter" jsonschema:"story か music"`
	Mode      string `json:"mode" jsonschema:"auto, manual, lock"`
}

// View is the World State screen.
type View struct {
	ProjectID    string                      `json:"project_id"`
	BranchID     string                      `json:"branch_id,omitempty"`
	BranchName   string                      `json:"branch_name,omitempty"`
	Version      int                         `json:"version" jsonschema:"最新の World State バージョン番号。まだ Shot が完了していなければ 0"`
	Characters   []domain.WorldCharacter     `json:"characters"`
	Products     []domain.WorldProduct       `json:"products"`
	Locations    []domain.WorldLocation      `json:"locations"`
	Camera       domain.WorldCamera          `json:"camera"`
	Story        domain.WorldStory           `json:"story"`
	Locks        []FieldLock                 `json:"locks" jsonschema:"LOCK されている項目（State Extractor は更新せず、編集もできない）"`
	ProjectLocks []ProjectLock               `json:"project_locks"`
	Library      []worldcommon.ReferenceItem `json:"library" jsonschema:"参照枠に割り当てられる Reference Library の画像"`
	Versions     []worldcommon.VersionEntry  `json:"versions" jsonschema:"本線の World State バージョン（新しい順）。restore_world_state_version で戻せる"`
}

func getWorldState(svc *service.WorldService) mcpkit.Handler[Scope, View] {
	return func(ctx context.Context, in Scope) (View, error) {
		v, err := svc.View(ctx, in.ProjectID, in.BranchID)
		if err != nil {
			return View{}, err
		}
		return ViewOf(v), nil
	}
}

// ViewOf is the World State screen as the tools return it.
func ViewOf(v service.WorldView) View {
	w := v.World
	out := View{
		ProjectID: v.Project.ID, Version: w.Version,
		Characters: nonNil(w.Characters), Products: nonNil(w.Products), Locations: nonNil(w.Locations),
		Camera: w.Camera, Story: w.Story,
		Locks: fieldLocks(v.Locks), ProjectLocks: projectLocks(v.ProjectLocks),
		Library: worldcommon.ReferenceItems(v.Library), Versions: worldcommon.VersionEntries(v.Versions),
	}
	if v.Branch != nil {
		out.BranchID, out.BranchName = v.Branch.ID, v.Branch.Name
	}
	return out
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func fieldLocks(set domain.LockSet) []FieldLock {
	out := make([]FieldLock, 0, len(set))
	for k := range set {
		out = append(out, FieldLock{TargetType: string(k.Target), TargetID: k.ID, Parameter: k.Field})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.TargetType != b.TargetType {
			return a.TargetType < b.TargetType
		}
		if a.TargetID != b.TargetID {
			return a.TargetID < b.TargetID
		}
		return a.Parameter < b.Parameter
	})
	return out
}

func projectLocks(locks domain.ParameterLocks) []ProjectLock {
	params := domain.ProjectLockParameters()
	out := make([]ProjectLock, 0, len(params))
	for _, p := range params {
		out = append(out, ProjectLock{Parameter: string(p), Mode: string(locks.Mode(p))})
	}
	return out
}
