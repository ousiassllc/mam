package references_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/references"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldtest"
)

func newEnv(t *testing.T) (*mcptest.Env, worldtest.Project) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		files.Register(r, env.Services)
		references.Register(r, env.Services)
	})
	return env, worldtest.Seed(t, env)
}

func TestListReferences_FiltersLikeTheLibraryScreen(t *testing.T) {
	env, p := newEnv(t)
	var out references.Library
	env.CallOK("list_references", map[string]any{"project_id": p.ID}, &out)
	assert.NotNil(t, out.Items)
	assert.Empty(t, out.Items)

	mio := worldtest.Reference(t, env, p.ID, "character", "Mio face")
	worldtest.Reference(t, env, p.ID, "style", "Noir")

	var all references.Library
	env.CallOK("list_references", map[string]any{"project_id": p.ID}, &all)
	assert.Len(t, all.Items, 2)

	var byKind references.Library
	env.CallOK("list_references", map[string]any{"project_id": p.ID, "kind": "character"}, &byKind)
	require.Len(t, byKind.Items, 1)
	assert.Equal(t, mio.ID, byKind.Items[0].ID)
	assert.Equal(t, mio.AssetID, byKind.Items[0].AssetID)
	assert.Equal(t, "uploaded", byKind.Items[0].Source)

	var byName references.Library
	env.CallOK("list_references", map[string]any{"project_id": p.ID, "q": "noir"}, &byName)
	require.Len(t, byName.Items, 1)
	assert.Equal(t, "Noir", byName.Items[0].Name)

	assert.Contains(t, env.CallError("list_references", map[string]any{"project_id": p.ID, "kind": "keyframe"}), "validation:", "a keyframe belongs to one Shot, not the Library")
	assert.Contains(t, env.CallError("list_references", map[string]any{"project_id": "nope"}), "not_found")
}

func TestReferenceImage_UploadedThroughATicketIsListedAndDownloadable(t *testing.T) {
	env, p := newEnv(t)
	var ticket mcpkit.UploadTicket
	env.CallOK("create_upload_ticket", map[string]any{"purpose": "reference_image", "params": map[string]string{"project_id": p.ID, "kind": "style", "name": "Noir", "tags": "dark, film"}}, &ticket)
	assert.NotEmpty(t, ticket.URL)

	item := worldtest.Reference(t, env, p.ID, "location", "Alley")
	var link mcpkit.Link
	env.CallOK("get_asset_download_url", map[string]any{"project_id": p.ID, "asset_id": item.AssetID}, &link)
	assert.NotEmpty(t, link.URL, "a Library image is an asset of the Project: the foundation hands it out")
}

func TestDeleteReference_RemovesTheEntryOnly(t *testing.T) {
	env, p := newEnv(t)
	keep := worldtest.Reference(t, env, p.ID, "character", "keep")
	drop := worldtest.Reference(t, env, p.ID, "character", "drop")

	var out references.Library
	env.CallOK("delete_reference", map[string]any{"project_id": p.ID, "reference_id": drop.ID}, &out)
	require.Len(t, out.Items, 1)
	assert.Equal(t, keep.ID, out.Items[0].ID)
	assert.Equal(t, 1, worldtest.Count(t, env, `SELECT COUNT(*) FROM asset WHERE id = ?`, drop.AssetID), "the image itself stays")

	assert.Contains(t, env.CallError("delete_reference", map[string]any{"project_id": p.ID, "reference_id": drop.ID}), "not_found")
	other := worldtest.SeedProject(t, env)
	assert.Contains(t, env.CallError("delete_reference", map[string]any{"project_id": other.ID, "reference_id": keep.ID}), "not_found", "an entry of another Project")
}

func TestPinReference_AddsTheImageToTheReferencePackOfTheShot(t *testing.T) {
	env, p := newEnv(t)
	style := worldtest.Reference(t, env, p.ID, "style", "Noir")

	var out references.Pack
	env.CallOK("pin_reference", map[string]any{"shot_id": p.ShotID, "reference_id": style.ID}, &out)
	require.Len(t, out.References, 1)
	assert.Equal(t, "style", out.References[0].Kind)
	assert.Equal(t, style.AssetID, out.References[0].AssetID)
	assert.Equal(t, style.ID, out.References[0].LibraryItem)

	var again references.Pack
	env.CallOK("pin_reference", map[string]any{"shot_id": p.ShotID, "reference_id": style.ID}, &again)
	assert.Len(t, again.References, 1, "a Pack has one Style image")
}

func TestPinReference_Refusals(t *testing.T) {
	env, p := newEnv(t)
	style := worldtest.Reference(t, env, p.ID, "style", "Noir")
	assert.Contains(t, env.CallError("pin_reference", map[string]any{"shot_id": p.ShotID, "reference_id": " "}), "validation:")
	assert.Contains(t, env.CallError("pin_reference", map[string]any{"shot_id": "nope", "reference_id": style.ID}), "not_found")
	assert.Contains(t, env.CallError("pin_reference", map[string]any{"shot_id": p.ShotID, "reference_id": "nope"}), "not_found")

	other := worldtest.SeedProject(t, env)
	foreign := worldtest.Reference(t, env, other.ID, "style", "Other")
	assert.Contains(t, env.CallError("pin_reference", map[string]any{"shot_id": p.ShotID, "reference_id": foreign.ID}), "not_found", "an entry of another Project")

	worldtest.Exec(t, env, `UPDATE shot SET state = 'GENERATING' WHERE id = ?`, p.ShotID)
	assert.Contains(t, env.CallError("pin_reference", map[string]any{"shot_id": p.ShotID, "reference_id": style.ID}), "conflict", "a Shot being generated")
}
