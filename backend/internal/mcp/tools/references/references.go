// Package references holds the MCP tools of the Reference Library screen (/projects/:id/references): the
// list, the removal of an image, and pinning an image to the Reference Pack of a Shot. Uploading an
// image is the foundation's upload purpose reference_image (create_upload_ticket); the image itself
// is handed out by get_asset_download_url (asset_id).
package references

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldcommon"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Read(r, "list_references",
		"Project の Reference Library の画像一覧。q（名前・タグの一部）・kind・tag で絞り込める / List the Reference Library of a Project, optionally filtered. 画像の登録は create_upload_ticket（purpose=reference_image）、画像の取得は get_asset_download_url。",
		list(svc.Reference))
	mcpkit.Destroy(r, "delete_reference",
		"Reference Library から画像を外す（画像そのものと、Shot が持つ参照は残る）/ Take an image out of the Reference Library (the image and the references Shots hold stay).",
		remove(svc.Reference))
	mcpkit.Write(r, "pin_reference",
		"Reference Library の画像を Shot の Reference Pack に加える。同じ種別・名前の参照は置き換わる。生成中の Shot は conflict / Add a Library image to the Reference Pack of a Shot.",
		pin(svc.Reference))
}

// ListInput filters the Library.
type ListInput struct {
	ProjectID string `json:"project_id"`
	Q         string `json:"q,omitempty" jsonschema:"名前またはタグに含まれる文字（大文字小文字は区別しない）"`
	Kind      string `json:"kind,omitempty" jsonschema:"character, product, location, style, lighting のいずれか"`
	Tag       string `json:"tag,omitempty" jsonschema:"タグ 1 つ（完全一致）"`
}

// Library is the Reference Library listing.
type Library struct {
	ProjectID string                      `json:"project_id"`
	Items     []worldcommon.ReferenceItem `json:"items" jsonschema:"新しい登録が先"`
}

func list(svc *service.ReferenceService) mcpkit.Handler[ListInput, Library] {
	return func(ctx context.Context, in ListInput) (Library, error) {
		v, err := svc.Library(ctx, in.ProjectID, service.LibraryQuery{Q: in.Q, Kind: in.Kind, Tag: in.Tag})
		return library(v), err
	}
}

func library(v service.LibraryView) Library {
	return Library{ProjectID: v.Project.ID, Items: worldcommon.ReferenceItems(v.Items)}
}

// DeleteInput names the entry; the filter is not needed (the result is the whole Library).
type DeleteInput struct {
	ProjectID   string `json:"project_id"`
	ReferenceID string `json:"reference_id"`
}

func remove(svc *service.ReferenceService) mcpkit.Handler[DeleteInput, Library] {
	return func(ctx context.Context, in DeleteInput) (Library, error) {
		v, err := svc.Remove(ctx, in.ProjectID, in.ReferenceID, service.LibraryQuery{})
		return library(v), err
	}
}

// PinInput names the Shot and the Library image.
type PinInput struct {
	ShotID      string `json:"shot_id"`
	ReferenceID string `json:"reference_id" jsonschema:"Reference Library の ID（同じ Project のもの）"`
}

// Pack is the Reference Pack of a Shot after pinning.
type Pack struct {
	ShotID     string                      `json:"shot_id"`
	References []worldcommon.PackReference `json:"references"`
}

func pin(svc *service.ReferenceService) mcpkit.Handler[PinInput, Pack] {
	return func(ctx context.Context, in PinInput) (Pack, error) {
		v, err := svc.Pin(ctx, in.ShotID, in.ReferenceID)
		if err != nil {
			return Pack{}, err
		}
		return Pack{ShotID: in.ShotID, References: worldcommon.PackReferences(v.Pack)}, nil
	}
}
