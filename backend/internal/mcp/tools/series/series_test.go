package series_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/series"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldstate"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldtest"
)

func newEnv(t *testing.T) (*mcptest.Env, worldtest.Project) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		series.Register(r, env.Services)
		worldstate.Register(r, env.Services)
	})
	return env, worldtest.Seed(t, env)
}

func TestSeries_CreateFromProjectStartEpisodesAndUpdateCanonWithoutPropagation(t *testing.T) {
	env, p := newEnv(t)
	env.CallOK("update_character", map[string]any{"project_id": p.ID, "character_id": "c1", "hairstyle": "bob", "emotion": "angry"}, nil)

	var made series.CreateOutput
	env.CallOK("create_series", map[string]any{"name": "Mio Diaries", "from_project_id": p.ID}, &made)
	assert.Equal(t, "Mio Diaries", made.Name)

	var view series.View
	env.CallOK("get_series", map[string]any{"series_id": made.SeriesID}, &view)
	require.Len(t, view.Canon.Characters, 1)
	canon := view.Canon.Characters[0]
	assert.Equal(t, "Mio", canon.Name)
	assert.Equal(t, "bob", canon.Hairstyle)
	assert.Empty(t, canon.Emotion, "what a Shot leaves is not canon")
	assert.NotEqual(t, "c1", canon.ID, "the canon has its own row")
	require.Len(t, view.Episodes, 1)
	assert.Equal(t, p.ID, view.Episodes[0].ProjectID)
	assert.Equal(t, 1, view.Episodes[0].EpisodeNumber)
	assert.NotNil(t, view.Templates)
	assert.NotEmpty(t, view.Versions)

	var ep2 series.CreateEpisodeOutput
	env.CallOK("create_episode", map[string]any{"series_id": made.SeriesID, "previous_episode_id": p.ID}, &ep2)
	assert.Equal(t, 2, ep2.EpisodeNumber)
	var ws worldstate.View
	env.CallOK("get_world_state", map[string]any{"project_id": ep2.ProjectID}, &ws)
	require.Len(t, ws.Characters, 1)
	assert.Equal(t, "angry", ws.Characters[0].Emotion, "the Episode starts from the final World State of the previous one")

	var updated series.View
	env.CallOK("update_series_character", map[string]any{"series_id": made.SeriesID, "character_id": canon.ID, "hairstyle": " long "}, &updated)
	assert.Equal(t, "long", updated.Canon.Characters[0].Hairstyle)
	assert.Greater(t, updated.Version, view.Version, "the canon has a new version")
	env.CallOK("get_world_state", map[string]any{"project_id": ep2.ProjectID}, &ws)
	assert.Equal(t, "bob", ws.Characters[0].Hairstyle, "an existing Episode keeps what it copied")

	env.CallOK("update_series_style", map[string]any{"series_id": made.SeriesID, "style": "film noir"}, &updated)
	assert.Equal(t, "film noir", updated.Canon.Style)
}

func TestCreateSeries_DefaultsTheNameAndRefusesWhatTheScreenRefuses(t *testing.T) {
	env, p := newEnv(t)
	assert.Contains(t, env.CallError("create_series", map[string]any{}), "validation:", "no name and no Project")
	assert.Contains(t, env.CallError("create_series", map[string]any{"name": strings.Repeat("あ", 101)}), "validation:")
	assert.Contains(t, env.CallError("create_series", map[string]any{"name": "x", "from_project_id": "nope"}), "not_found")

	var made series.CreateOutput
	env.CallOK("create_series", map[string]any{"from_project_id": p.ID}, &made)
	assert.NotEmpty(t, made.Name, "the name of the Project")
	assert.Contains(t, env.CallError("create_series", map[string]any{"name": "again", "from_project_id": p.ID}), "validation:", "the Project already belongs to a Series")

	var empty series.CreateOutput
	env.CallOK("create_series", map[string]any{"name": "  Plain   Series "}, &empty)
	assert.Equal(t, "Plain Series", empty.Name)
	var view series.View
	env.CallOK("get_series", map[string]any{"series_id": empty.SeriesID}, &view)
	assert.Empty(t, view.Episodes)
	assert.NotNil(t, view.Canon.Characters)
}

func TestSeriesCanonEdits_Refusals(t *testing.T) {
	env, p := newEnv(t)
	var made series.CreateOutput
	env.CallOK("create_series", map[string]any{"name": "S", "from_project_id": p.ID}, &made)
	var view series.View
	env.CallOK("get_series", map[string]any{"series_id": made.SeriesID}, &view)
	id := view.Canon.Characters[0].ID

	assert.Contains(t, env.CallError("get_series", map[string]any{"series_id": "nope"}), "not_found")
	assert.Contains(t, env.CallError("update_series_character", map[string]any{"series_id": made.SeriesID, "character_id": id}), "validation:", "nothing to update")
	assert.Contains(t, env.CallError("update_series_character", map[string]any{"series_id": made.SeriesID, "character_id": id, "voice": strings.Repeat("あ", 201)}), "validation:")
	assert.Contains(t, env.CallError("update_series_character", map[string]any{"series_id": made.SeriesID, "character_id": "nope", "voice": "low"}), "not_found")
	assert.Contains(t, env.CallError("update_series_character", map[string]any{"series_id": "nope", "character_id": id, "voice": "low"}), "not_found")
	assert.Contains(t, env.CallError("update_series_style", map[string]any{"series_id": made.SeriesID, "style": strings.Repeat("あ", 501)}), "validation:")
	assert.Contains(t, env.CallError("update_series_style", map[string]any{"series_id": "nope", "style": "x"}), "not_found")

	assert.Contains(t, env.CallError("create_episode", map[string]any{"series_id": "nope"}), "not_found")
	assert.Contains(t, env.CallError("create_episode", map[string]any{"series_id": made.SeriesID, "brand_template_id": "nope"}), "validation:")
	other := worldtest.SeedProject(t, env)
	assert.Contains(t, env.CallError("create_episode", map[string]any{"series_id": made.SeriesID, "previous_episode_id": other.ID}), "validation:", "not an Episode of this Series")
}
