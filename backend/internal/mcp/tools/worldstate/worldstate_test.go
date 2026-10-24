package worldstate_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldstate"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldtest"
)

func newEnv(t *testing.T) (*mcptest.Env, worldtest.Project) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { worldstate.Register(r, env.Services) })
	return env, worldtest.Seed(t, env)
}

func TestGetWorldState_ShowsTheEntitiesLocksAndLibraryOfTheScreen(t *testing.T) {
	env, p := newEnv(t)
	var out worldstate.View
	env.CallOK("get_world_state", map[string]any{"project_id": p.ID}, &out)
	require.Len(t, out.Characters, 1)
	assert.Equal(t, "Mio", out.Characters[0].Name)
	require.Len(t, out.Products, 1)
	require.Len(t, out.Locations, 1)
	assert.Equal(t, "dusk", out.Locations[0].Time)
	assert.NotNil(t, out.Locks)
	assert.NotNil(t, out.Library)
	assert.NotNil(t, out.Versions)
	assert.Equal(t, []string{"story", "music"}, []string{out.ProjectLocks[0].Parameter, out.ProjectLocks[1].Parameter})
	assert.Equal(t, "auto", out.ProjectLocks[0].Mode)

	assert.Contains(t, env.CallError("get_world_state", map[string]any{"project_id": "nope"}), "not_found")
	assert.Contains(t, env.CallError("get_world_state", map[string]any{"project_id": p.ID, "branch_id": "nope"}), "not_found", "a Branch that is not in the Project")
}

func TestUpdateEntity_WritesTheGivenAttributesAndKeepsTheRest(t *testing.T) {
	env, p := newEnv(t)
	var out worldstate.View
	env.CallOK("update_character", map[string]any{"project_id": p.ID, "character_id": "c1", "wardrobe": "red_dress", "emotion": "angry"}, &out)
	assert.Equal(t, "red_dress", out.Characters[0].Wardrobe)
	assert.Equal(t, "angry", out.Characters[0].Emotion)
	assert.NotEmpty(t, out.Versions, "an edit saves a World State version")

	var cleared worldstate.View
	env.CallOK("update_character", map[string]any{"project_id": p.ID, "character_id": "c1", "emotion": ""}, &cleared)
	assert.Empty(t, cleared.Characters[0].Emotion, "an empty text clears the attribute")
	assert.Equal(t, "red_dress", cleared.Characters[0].Wardrobe, "an attribute left out stays")

	env.CallOK("update_product", map[string]any{"project_id": p.ID, "product_id": "p1", "material": "glass"}, &out)
	assert.Equal(t, "glass", out.Products[0].Material)
	env.CallOK("update_location", map[string]any{"project_id": p.ID, "location_id": "l1", "weather": "rain"}, &out)
	assert.Equal(t, "rain", out.Locations[0].Weather)
	assert.Equal(t, "dusk", out.Locations[0].Time)
}

func TestUpdateEntity_RefusesWhatTheScreenRefuses(t *testing.T) {
	env, p := newEnv(t)
	assert.Contains(t, env.CallError("update_character", map[string]any{"project_id": p.ID, "character_id": "c1"}), "validation:", "nothing to update")
	long := strings.Repeat("あ", 201)
	assert.Contains(t, env.CallError("update_character", map[string]any{"project_id": p.ID, "character_id": "c1", "voice": long}), "validation:")
	assert.Contains(t, env.CallError("update_character", map[string]any{"project_id": p.ID, "character_id": "nope", "voice": "low"}), "not_found")
	assert.Contains(t, env.CallError("update_product", map[string]any{"project_id": "nope", "product_id": "p1", "material": "x"}), "not_found")
	assert.Contains(t, env.CallError("update_location", map[string]any{"project_id": p.ID, "location_id": "l1", "time": "x", "branch_id": "nope"}), "not_found")
}

func TestSetWorldFieldLock_LockedAttributesCannotBeEditedUntilUnlocked(t *testing.T) {
	env, p := newEnv(t)
	lock := func(mode string) worldstate.View {
		var out worldstate.View
		env.CallOK("set_world_field_lock", map[string]any{"project_id": p.ID, "target_type": "character", "target_id": "c1", "parameter": "wardrobe", "mode": mode}, &out)
		return out
	}
	out := lock("lock")
	require.Len(t, out.Locks, 1)
	assert.Equal(t, worldstate.FieldLock{TargetType: "character", TargetID: "c1", Parameter: "wardrobe"}, out.Locks[0])

	assert.Contains(t, env.CallError("update_character", map[string]any{"project_id": p.ID, "character_id": "c1", "wardrobe": "x"}), "locked")
	assert.Zero(t, worldtest.Count(t, env, `SELECT COUNT(*) FROM character WHERE id = 'c1' AND wardrobe = 'x'`), "nothing was written")
	var edited worldstate.View
	env.CallOK("update_character", map[string]any{"project_id": p.ID, "character_id": "c1", "hairstyle": "bob"}, &edited)
	assert.Equal(t, "bob", edited.Characters[0].Hairstyle, "only the locked attribute is held back")

	assert.Empty(t, lock("auto").Locks)
	env.CallOK("update_character", map[string]any{"project_id": p.ID, "character_id": "c1", "wardrobe": "x"}, nil)
}

func TestSetWorldFieldLock_RefusesWhatCannotBeLocked(t *testing.T) {
	env, p := newEnv(t)
	call := func(args map[string]any) string {
		args["project_id"] = p.ID
		return env.CallError("set_world_field_lock", args)
	}
	assert.Contains(t, call(map[string]any{"target_type": "dog", "parameter": "wardrobe", "mode": "lock"}), "validation:")
	assert.Contains(t, call(map[string]any{"target_type": "character", "target_id": "c1", "parameter": "shoe_size", "mode": "lock"}), "validation:")
	assert.Contains(t, call(map[string]any{"target_type": "character", "target_id": "c1", "parameter": "wardrobe", "mode": "manual"}), "validation:")
	assert.Contains(t, call(map[string]any{"target_type": "story", "parameter": "previous_event", "mode": "lock", "branch_id": "nope"}), "not_found")
}

func TestAssignReference_ChecksKindSlotAndLock(t *testing.T) {
	env, p := newEnv(t)
	face := worldtest.Reference(t, env, p.ID, "character", "Mio face")
	style := worldtest.Reference(t, env, p.ID, "style", "Noir")

	var out worldstate.View
	env.CallOK("assign_character_reference", map[string]any{"project_id": p.ID, "entity_id": "c1", "slot": "face", "reference_id": face.ID}, &out)
	assert.Equal(t, face.AssetID, out.Characters[0].FaceReferenceAssetID)
	assert.Len(t, out.Library, 2)

	assert.Contains(t, env.CallError("assign_character_reference", map[string]any{"project_id": p.ID, "entity_id": "c1", "slot": "face", "reference_id": style.ID}), "validation:", "a Style image does not fit a face slot")
	assert.Contains(t, env.CallError("assign_character_reference", map[string]any{"project_id": p.ID, "entity_id": "c1", "slot": "geometry", "reference_id": face.ID}), "validation:", "a Character has no geometry slot")
	assert.Contains(t, env.CallError("assign_product_reference", map[string]any{"project_id": p.ID, "entity_id": "p1", "slot": "label", "reference_id": "nope"}), "not_found")

	env.CallOK("set_world_field_lock", map[string]any{"project_id": p.ID, "target_type": "character", "target_id": "c1", "parameter": "identity", "mode": "lock"}, nil)
	assert.Contains(t, env.CallError("assign_character_reference", map[string]any{"project_id": p.ID, "entity_id": "c1", "slot": "face"}), "locked", "the identity LOCK freezes the slot")
	env.CallOK("set_world_field_lock", map[string]any{"project_id": p.ID, "target_type": "character", "target_id": "c1", "parameter": "identity", "mode": "auto"}, nil)

	var cleared worldstate.View
	env.CallOK("assign_character_reference", map[string]any{"project_id": p.ID, "entity_id": "c1", "slot": "face"}, &cleared)
	assert.Empty(t, cleared.Characters[0].FaceReferenceAssetID, "an empty reference_id clears the slot")
}

func TestAssignReference_ProductAndLocationSlots(t *testing.T) {
	env, p := newEnv(t)
	product := worldtest.Reference(t, env, p.ID, "product", "Serum bottle")
	location := worldtest.Reference(t, env, p.ID, "location", "Street plan")
	var out worldstate.View
	env.CallOK("assign_product_reference", map[string]any{"project_id": p.ID, "entity_id": "p1", "slot": "geometry", "reference_id": product.ID}, &out)
	assert.Equal(t, product.AssetID, out.Products[0].GeometryReferenceAssetID)
	env.CallOK("assign_location_reference", map[string]any{"project_id": p.ID, "entity_id": "l1", "slot": "layout", "reference_id": location.ID}, &out)
	assert.Equal(t, location.AssetID, out.Locations[0].LayoutReferenceAssetID)
	assert.Contains(t, env.CallError("assign_location_reference", map[string]any{"project_id": p.ID, "entity_id": "l1", "slot": "layout", "reference_id": product.ID}), "validation:")
}
