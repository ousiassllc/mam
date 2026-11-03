package character_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/character"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/series"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldstate"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldtest"
)

func newEnv(t *testing.T) (*mcptest.Env, worldtest.Project) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		character.Register(r, env.Services)
		series.Register(r, env.Services)
		worldstate.Register(r, env.Services)
	})
	return env, worldtest.Seed(t, env)
}

// A Project whose planning never made a Character (the LLM was not set up) can still have one: made with its attributes and a face
// from the Library, the returned id is what assign_character_reference takes.
func TestCreateCharacter_InAProjectGivesTheIDAndTakesTheFaceAtCreation(t *testing.T) {
	env, p := newEnv(t)
	face := worldtest.Reference(t, env, p.ID, "character", "Aoi face")
	body := worldtest.Reference(t, env, p.ID, "character", "Aoi body")

	var out character.Output
	env.CallOK("create_character", map[string]any{
		"project_id": p.ID, "name": " Aoi ", "hairstyle": "ponytail", "wardrobe": "school", "personality": "calm", "voice": "soft",
		"face_reference_id": face.ID, "body_reference_id": body.ID,
	}, &out)
	assert.NotEmpty(t, out.CharacterID)
	assert.Equal(t, p.ID, out.ProjectID)
	assert.Equal(t, "Aoi", out.Character.Name, "the name is trimmed")
	assert.Equal(t, "ponytail", out.Character.Hairstyle)
	assert.Equal(t, face.AssetID, out.Character.FaceReferenceAssetID)
	assert.Equal(t, body.AssetID, out.Character.BodyReferenceAssetID)
	assert.Positive(t, out.Version, "the World State has a new version")

	var ws worldstate.View
	env.CallOK("get_world_state", map[string]any{"project_id": p.ID}, &ws)
	require.Len(t, ws.Characters, 2)
	assert.Equal(t, 1, worldtest.Count(t, env, `SELECT COUNT(*) FROM character WHERE id = ? AND project_id = ?`, out.CharacterID, p.ID))

	var assigned worldstate.View
	env.CallOK("assign_character_reference", map[string]any{"project_id": p.ID, "entity_id": out.CharacterID, "slot": "face", "reference_id": face.ID}, &assigned)
	assert.Len(t, assigned.Characters, 2, "the id works with the tools of the World State")
}

func TestCreateCharacter_WithoutAFaceIsEnoughAndTheFaceCanComeLater(t *testing.T) {
	env, p := newEnv(t)
	var out character.Output
	env.CallOK("create_character", map[string]any{"project_id": p.ID, "name": "Ren"}, &out)
	assert.Empty(t, out.Character.FaceReferenceAssetID)
	face := worldtest.Reference(t, env, p.ID, "character", "Ren face")
	var ws worldstate.View
	env.CallOK("assign_character_reference", map[string]any{"project_id": p.ID, "entity_id": out.CharacterID, "slot": "face", "reference_id": face.ID}, &ws)
	for _, c := range ws.Characters {
		if c.ID == out.CharacterID {
			assert.Equal(t, face.AssetID, c.FaceReferenceAssetID)
		}
	}
}

func TestCreateCharacter_RefusesWhatTheScreenRefuses(t *testing.T) {
	env, p := newEnv(t)
	style := worldtest.Reference(t, env, p.ID, "style", "Noir")
	call := func(args map[string]any) string { return env.CallError("create_character", args) }

	assert.Contains(t, call(map[string]any{"name": "X"}), "validation:", "neither project_id nor series_id")
	assert.Contains(t, call(map[string]any{"project_id": p.ID, "series_id": "s", "name": "X"}), "validation:", "both")
	assert.Contains(t, call(map[string]any{"project_id": p.ID, "name": "  "}), "validation:", "no name")
	assert.Contains(t, call(map[string]any{"project_id": p.ID, "name": strings.Repeat("あ", 101)}), "validation:", "name too long")
	assert.Contains(t, call(map[string]any{"project_id": p.ID, "name": "X", "voice": strings.Repeat("あ", 201)}), "validation:", "text too long")
	assert.Contains(t, call(map[string]any{"project_id": p.ID, "name": "mio"}), "validation:", "Mio is there (case is not told apart)")
	assert.Contains(t, call(map[string]any{"project_id": p.ID, "name": "X", "face_reference_id": style.ID}), "validation:", "a Style image is no face")
	assert.Contains(t, call(map[string]any{"project_id": p.ID, "name": "X", "face_reference_id": "nope"}), "not_found")
	assert.Contains(t, call(map[string]any{"project_id": "nope", "name": "X"}), "not_found")
	assert.Contains(t, call(map[string]any{"project_id": p.ID, "name": "X", "branch_id": "nope"}), "not_found")
	assert.Contains(t, call(map[string]any{"project_id": p.ID, "name": "X", "reference_project_id": p.ID}), "validation:", "reference_project_id is for a Series")
	assert.Zero(t, worldtest.Count(t, env, `SELECT COUNT(*) FROM character WHERE name = 'X'`), "a refused call writes nothing")
}

// The canon of a Series made from nothing has no Character; one made by hand is in the canon, in a new version, and the next Episode gets it.
func TestCreateCharacter_InTheCanonOfASeriesStartsFromNothingAndReachesTheNextEpisode(t *testing.T) {
	env, p := newEnv(t)
	face := worldtest.Reference(t, env, p.ID, "character", "Kana face")
	var made series.CreateOutput
	env.CallOK("create_series", map[string]any{"name": "仮AIインフルエンサー"}, &made)
	var before series.View
	env.CallOK("get_series", map[string]any{"series_id": made.SeriesID}, &before)
	require.Empty(t, before.Canon.Characters)

	var out character.Output
	env.CallOK("create_character", map[string]any{
		"series_id": made.SeriesID, "name": "Kana", "hairstyle": "bob", "personality": "bright",
		"face_reference_id": face.ID, "reference_project_id": p.ID,
	}, &out)
	assert.Equal(t, made.SeriesID, out.SeriesID)
	assert.Equal(t, face.AssetID, out.Character.FaceReferenceAssetID)
	assert.Greater(t, out.Version, before.Version, "the canon has a new version")

	var after series.View
	env.CallOK("get_series", map[string]any{"series_id": made.SeriesID}, &after)
	require.Len(t, after.Canon.Characters, 1)
	assert.Equal(t, out.CharacterID, after.Canon.Characters[0].ID)
	assert.Equal(t, "bob", after.Canon.Characters[0].Hairstyle)

	var updated series.View
	env.CallOK("update_series_character", map[string]any{"series_id": made.SeriesID, "character_id": out.CharacterID, "wardrobe": "coat"}, &updated)
	assert.Equal(t, "coat", updated.Canon.Characters[0].Wardrobe, "the id works with the tools of the canon")

	var ep series.CreateEpisodeOutput
	env.CallOK("create_episode", map[string]any{"series_id": made.SeriesID}, &ep)
	var ws worldstate.View
	env.CallOK("get_world_state", map[string]any{"project_id": ep.ProjectID}, &ws)
	require.Len(t, ws.Characters, 1)
	assert.Equal(t, "Kana", ws.Characters[0].Name)
	assert.Equal(t, "coat", ws.Characters[0].Wardrobe)
	assert.NotEmpty(t, ws.Characters[0].FaceReferenceAssetID, "the Episode has its own copy of the face")
}

func TestCreateCharacter_InTheCanonRefusesWhatCannotBeKept(t *testing.T) {
	env, p := newEnv(t)
	face := worldtest.Reference(t, env, p.ID, "character", "Kana face")
	style := worldtest.Reference(t, env, p.ID, "style", "Noir")
	var made series.CreateOutput
	env.CallOK("create_series", map[string]any{"name": "S"}, &made)
	env.CallOK("create_character", map[string]any{"series_id": made.SeriesID, "name": "Kana"}, nil)
	call := func(args map[string]any) string {
		args["series_id"] = made.SeriesID
		return env.CallError("create_character", args)
	}
	assert.Contains(t, call(map[string]any{"name": "kana"}), "validation:", "the name is in the canon")
	assert.Contains(t, call(map[string]any{"name": "Y", "face_reference_id": face.ID}), "validation:", "an image needs the Project of its Library")
	assert.Contains(t, call(map[string]any{"name": "Y", "face_reference_id": style.ID, "reference_project_id": p.ID}), "validation:")
	assert.Contains(t, call(map[string]any{"name": "Y", "face_reference_id": face.ID, "reference_project_id": "nope"}), "not_found")
	assert.Contains(t, call(map[string]any{"name": "Y", "branch_id": "b"}), "validation:", "a canon has no Branch")
	assert.Contains(t, env.CallError("create_character", map[string]any{"series_id": "nope", "name": "Y"}), "not_found")
	assert.Equal(t, 1, worldtest.Count(t, env, `SELECT COUNT(*) FROM character WHERE series_id = ?`, made.SeriesID))
}

// A draft (made from an attached image when the planning could not run, #1022) is told as such by get_world_state and stays one until the
// user edits it with update_character; a Character made by hand carries no draft mark.
func TestCreateCharacter_TheDraftMarkIsShownByTheWorldStateAndClearedByAnEdit(t *testing.T) {
	env, p := newEnv(t)
	worldtest.Exec(t, env, `INSERT INTO character (id, project_id, name, draft) VALUES ('d1', ?, '登場人物 1', 1)`, p.ID)
	var made character.Output
	env.CallOK("create_character", map[string]any{"project_id": p.ID, "name": "Ren"}, &made)
	assert.False(t, made.Character.Draft, "a Character made by hand is no draft")

	drafts := func() map[string]bool {
		var ws worldstate.View
		env.CallOK("get_world_state", map[string]any{"project_id": p.ID}, &ws)
		out := map[string]bool{}
		for _, c := range ws.Characters {
			out[c.ID] = c.Draft
		}
		return out
	}
	assert.Equal(t, map[string]bool{"c1": false, "d1": true, made.CharacterID: false}, drafts())

	env.CallOK("update_character", map[string]any{"project_id": p.ID, "character_id": "d1", "hairstyle": "bob"}, nil)

	assert.Equal(t, map[string]bool{"c1": false, "d1": false, made.CharacterID: false}, drafts(), "the edit clears the mark")
}

func TestCreateCharacter_TheBranchRefusalSaysBranchIsForAProject(t *testing.T) {
	env, _ := newEnv(t)
	var made series.CreateOutput
	env.CallOK("create_series", map[string]any{"name": "S"}, &made)

	msg := env.CallError("create_character", map[string]any{"series_id": made.SeriesID, "name": "Y", "branch_id": "b"})

	assert.Contains(t, msg, "branch_id は project_id のときだけ")
	assert.NotContains(t, msg, "series_id のときだけ")
}
