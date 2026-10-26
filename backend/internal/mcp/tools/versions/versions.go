// Package versions holds the MCP tools of the version histories (F-28): the saved versions of a Shot spec,
// of a World State (trunk or Branch) and of a Branch, and restoring one of them. A restore never deletes a
// version: it appends a copy of the chosen one, so it can be undone by restoring again.
package versions

import (
	"context"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldcommon"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldstate"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	v := svc.Versions
	mcpkit.Read(r, "list_shot_versions",
		"Shot の Spec の保存済みバージョン（新しい順）/ Saved spec versions of a Shot.",
		listShot(svc.Director))
	mcpkit.Read(r, "list_world_state_versions",
		"本線（または branch_id の Branch）の World State の保存済みバージョン（新しい順）/ Saved World State versions of the trunk or of a Branch.",
		listWorld(svc.World))
	mcpkit.Read(r, "get_branch_versions",
		"Branch 自身のバージョンと、Branch 専用の World State のバージョン（どちらも新しい順）/ Version history of a Branch and of its own World State series.",
		branchHistory(v))
	mcpkit.Write(r, "restore_shot_version",
		"Shot の Spec を保存済みのバージョンに戻す（新しいバージョンとして追加する）。LOCK 中の部分が変わるなら locked、生成中の Shot は conflict / Restore the spec of a Shot to a saved version.",
		restoreShot(v))
	mcpkit.Write(r, "restore_world_state_version",
		"World State を保存済みのバージョンに戻す（新しいバージョンとして追加する）。branch_id があればその Branch の World State。LOCK 中の項目が変わるなら locked で何も書かない / Restore the World State of the trunk or a Branch.",
		restoreWorld(v))
	mcpkit.Write(r, "restore_branch_version",
		"Branch 自身（名前と World State の参照）を保存済みのバージョンに戻す / Restore a Branch (name and World State reference) to a saved version.",
		restoreBranch(v.RestoreBranch))
	mcpkit.Write(r, "restore_branch_world_state_version",
		"Branch 専用の World State の系列を保存済みのバージョンに戻す。LOCK 中の項目が変わるなら locked / Restore the World State series of a Branch.",
		restoreBranch(v.RestoreBranchWorldState))
}

// ShotInput names the Shot.
type ShotInput struct {
	ShotID string `json:"shot_id"`
}

// List is a list of saved versions, newest first.
type List struct {
	Versions []worldcommon.VersionEntry `json:"versions"`
}

// listShot reads the version list the Shot Inspector shows.
func listShot(svc *service.DirectorService) mcpkit.Handler[ShotInput, List] {
	return func(ctx context.Context, in ShotInput) (List, error) {
		v, err := svc.Inspector(ctx, in.ShotID)
		return List{Versions: worldcommon.VersionEntries(v.Versions)}, err
	}
}

// WorldInput names the World State: the trunk of a Project, or one of its Branches.
type WorldInput struct {
	ProjectID string `json:"project_id"`
	BranchID  string `json:"branch_id,omitempty" jsonschema:"Branch の World State を扱うときの Branch ID。省略すると本線"`
}

// listWorld reads the version list the World State page shows (of the Branch when branch_id is set).
func listWorld(svc *service.WorldService) mcpkit.Handler[WorldInput, List] {
	return func(ctx context.Context, in WorldInput) (List, error) {
		v, err := svc.View(ctx, in.ProjectID, in.BranchID)
		return List{Versions: worldcommon.VersionEntries(v.Versions)}, err
	}
}

// BranchInput names the Branch.
type BranchInput struct {
	BranchID string `json:"branch_id"`
}

// BranchHistory is the version history of a Branch.
type BranchHistory struct {
	BranchID string                     `json:"branch_id"`
	Versions []worldcommon.VersionEntry `json:"versions" jsonschema:"Branch 自身（名前と World State の参照）のバージョン"`
	World    []worldcommon.VersionEntry `json:"world_state_versions" jsonschema:"Branch 専用の World State のバージョン"`
}

func historyOf(h service.BranchHistory) BranchHistory {
	return BranchHistory{BranchID: h.BranchID, Versions: worldcommon.VersionEntries(h.Versions), World: worldcommon.VersionEntries(h.World)}
}

func branchHistory(svc *service.VersionService) mcpkit.Handler[BranchInput, BranchHistory] {
	return func(ctx context.Context, in BranchInput) (BranchHistory, error) {
		h, err := svc.Branch(ctx, in.BranchID)
		return historyOf(h), err
	}
}

// RestoreShotInput names the Shot and the version.
type RestoreShotInput struct {
	ShotID        string `json:"shot_id"`
	VersionNumber int    `json:"version_number" jsonschema:"list_shot_versions の version_number（1 以上）"`
}

// RestoredShot is the Shot with its versions after the restore.
type RestoredShot struct {
	ShotID   string                     `json:"shot_id"`
	Versions []worldcommon.VersionEntry `json:"versions" jsonschema:"戻した結果を含む、新しい順のバージョン"`
}

func restoreShot(svc *service.VersionService) mcpkit.Handler[RestoreShotInput, RestoredShot] {
	return func(ctx context.Context, in RestoreShotInput) (RestoredShot, error) {
		number, err := service.ParseVersionNumber(strconv.Itoa(in.VersionNumber))
		if err != nil {
			return RestoredShot{}, err
		}
		v, err := svc.RestoreShot(ctx, in.ShotID, number)
		if err != nil {
			return RestoredShot{}, err
		}
		return RestoredShot{ShotID: in.ShotID, Versions: worldcommon.VersionEntries(v.Versions)}, nil
	}
}

// RestoreWorldInput names the World State and the version.
type RestoreWorldInput struct {
	WorldInput
	VersionNumber int `json:"version_number" jsonschema:"list_world_state_versions の version_number（1 以上）"`
}

func restoreWorld(svc *service.VersionService) mcpkit.Handler[RestoreWorldInput, worldstate.View] {
	return func(ctx context.Context, in RestoreWorldInput) (worldstate.View, error) {
		number, err := service.ParseVersionNumber(strconv.Itoa(in.VersionNumber))
		if err != nil {
			return worldstate.View{}, err
		}
		v, err := svc.RestoreWorldState(ctx, in.ProjectID, in.BranchID, number)
		if err != nil {
			return worldstate.View{}, err
		}
		return worldstate.ViewOf(v), nil
	}
}

// RestoreBranchInput names the Branch and the version.
type RestoreBranchInput struct {
	BranchID      string `json:"branch_id"`
	VersionNumber int    `json:"version_number" jsonschema:"get_branch_versions の version_number（1 以上）"`
}

func restoreBranch(restore func(ctx context.Context, branchID string, number int) (service.BranchHistory, error)) mcpkit.Handler[RestoreBranchInput, BranchHistory] {
	return func(ctx context.Context, in RestoreBranchInput) (BranchHistory, error) {
		number, err := service.ParseVersionNumber(strconv.Itoa(in.VersionNumber))
		if err != nil {
			return BranchHistory{}, err
		}
		h, err := restore(ctx, in.BranchID, number)
		if err != nil {
			return BranchHistory{}, err
		}
		return historyOf(h), nil
	}
}
