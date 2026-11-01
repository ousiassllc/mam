package voicereference_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editortest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/series"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/voicereference"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldstate"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/worldtest"
)

func wav(n int) []byte {
	return append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), bytes.Repeat([]byte{1}, n)...)
}

func newEnv(t *testing.T) (*mcptest.Env, worldtest.Project) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		series.Register(r, env.Services)
		worldstate.Register(r, env.Services)
		voicereference.Register(r, env.Services)
	})
	return env, worldtest.Seed(t, env)
}

func TestProjectCharacter_UploadUpdateDownloadAndDelete(t *testing.T) {
	env, p := newEnv(t)
	params := map[string]string{"project_id": p.ID, "character_id": "c1", "text": "こんにちは", "language": "ja"}

	rec := editortest.Upload(env, "character_voice_reference", params, "voice.wav", wav(64))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "file_path", "the stored file is not named")

	var ws worldstate.View
	env.CallOK("get_world_state", map[string]any{"project_id": p.ID}, &ws)
	require.NotNil(t, ws.Characters[0].VoiceReference, "the World State shows the reference voice")
	assert.Equal(t, "こんにちは", ws.Characters[0].VoiceReference.Text)
	assert.Equal(t, "audio/wav", ws.Characters[0].VoiceReference.MimeType)

	var out voicereference.Output
	env.CallOK("update_character_voice_reference", map[string]any{"project_id": p.ID, "character_id": "c1", "text": "さようなら", "language": "en"}, &out)
	require.NotNil(t, out.VoiceReference)
	assert.Equal(t, "さようなら", out.VoiceReference.Text)
	assert.Equal(t, "en", out.VoiceReference.Language)
	env.CallOK("update_character_voice_reference", map[string]any{"project_id": p.ID, "character_id": "c1"}, &out)
	assert.Equal(t, "さようなら", out.VoiceReference.Text, "what is not given stays")

	var link mcpkit.Link
	env.CallOK("get_character_voice_reference_url", map[string]any{"project_id": p.ID, "character_id": "c1"}, &link)
	dl := editortest.Download(env, link.URL)
	require.Equal(t, http.StatusOK, dl.Code)
	assert.Equal(t, "audio/wav", dl.Header().Get("Content-Type"))
	assert.Equal(t, wav(64), dl.Body.Bytes())

	env.CallOK("delete_character_voice_reference", map[string]any{"project_id": p.ID, "character_id": "c1"}, &out)
	assert.Nil(t, out.VoiceReference)
	assert.Contains(t, env.CallError("get_character_voice_reference_url", map[string]any{"project_id": p.ID, "character_id": "c1"}), "not_found")
	env.CallOK("delete_character_voice_reference", map[string]any{"project_id": p.ID, "character_id": "c1"}, &out)
}

func TestSeriesCharacter_TheCanonOwnsTheVoiceAndTheNextEpisodeInheritsIt(t *testing.T) {
	env, p := newEnv(t)
	var made series.CreateOutput
	env.CallOK("create_series", map[string]any{"name": "Mio Diaries", "from_project_id": p.ID}, &made)
	var view series.View
	env.CallOK("get_series", map[string]any{"series_id": made.SeriesID}, &view)
	canonID := view.Canon.Characters[0].ID

	rec := editortest.Upload(env, "character_voice_reference", map[string]string{"series_id": made.SeriesID, "character_id": canonID, "text": "正本"}, "voice.mp3", append([]byte("ID3"), make([]byte, 30)...))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var after series.View
	env.CallOK("get_series", map[string]any{"series_id": made.SeriesID}, &after)
	require.NotNil(t, after.Canon.Characters[0].VoiceReference, "get_series shows it")
	assert.Equal(t, "正本", after.Canon.Characters[0].VoiceReference.Text)
	assert.Greater(t, after.Version, view.Version, "the canon has a new version")

	var ep2 series.CreateEpisodeOutput
	env.CallOK("create_episode", map[string]any{"series_id": made.SeriesID}, &ep2)
	var ws worldstate.View
	env.CallOK("get_world_state", map[string]any{"project_id": ep2.ProjectID}, &ws)
	require.NotNil(t, ws.Characters[0].VoiceReference, "the Episode inherited the voice")

	// Removing the canon voice does not take it from the Episode that has it.
	var out voicereference.Output
	env.CallOK("delete_character_voice_reference", map[string]any{"series_id": made.SeriesID, "character_id": canonID}, &out)
	env.CallOK("get_world_state", map[string]any{"project_id": ep2.ProjectID}, &ws)
	assert.NotNil(t, ws.Characters[0].VoiceReference)

	var link mcpkit.Link
	assert.Contains(t, env.CallError("get_character_voice_reference_url", map[string]any{"series_id": made.SeriesID, "character_id": canonID}), "not_found")
	env.CallOK("get_character_voice_reference_url", map[string]any{"project_id": ep2.ProjectID, "character_id": ws.Characters[0].ID}, &link)
	assert.Equal(t, http.StatusOK, editortest.Download(env, link.URL).Code)
}

func TestVoiceReferenceTools_RefuseWhatTheScreenRefuses(t *testing.T) {
	env, p := newEnv(t)
	ok := map[string]string{"project_id": p.ID, "character_id": "c1"}

	_, err := env.Registrar.Files.UploadTicketFor(t.Context(), "character_voice_reference", map[string]string{"project_id": p.ID})
	require.Error(t, err, "a Character is required")

	for name, c := range map[string]struct {
		params map[string]string
		file   string
		data   []byte
		status int
	}{
		"neither series nor project": {map[string]string{"character_id": "c1"}, "v.wav", wav(8), http.StatusUnprocessableEntity},
		"both series and project":    {map[string]string{"project_id": p.ID, "series_id": "s", "character_id": "c1"}, "v.wav", wav(8), http.StatusUnprocessableEntity},
		"not audio":                  {ok, "v.wav", []byte("MZ\x90\x00 not audio"), http.StatusUnprocessableEntity},
		"an unsupported type":        {ok, "v.txt", []byte("hello"), http.StatusUnprocessableEntity},
		"an empty file":              {ok, "v.wav", nil, http.StatusUnprocessableEntity},
		"a language that is none":    {map[string]string{"project_id": p.ID, "character_id": "c1", "language": "xx"}, "v.wav", wav(8), http.StatusUnprocessableEntity},
		"an unknown Project":         {map[string]string{"project_id": "nope", "character_id": "c1"}, "v.wav", wav(8), http.StatusNotFound},
		"an unknown Character":       {map[string]string{"project_id": p.ID, "character_id": "nope"}, "v.wav", wav(8), http.StatusNotFound},
		"an unknown Series":          {map[string]string{"series_id": "nope", "character_id": "c1"}, "v.wav", wav(8), http.StatusNotFound},
	} {
		assert.Equal(t, c.status, editortest.Upload(env, "character_voice_reference", c.params, c.file, c.data).Code, name)
	}

	spec := uploadSpec(t, env.Registrar.Files)
	assert.EqualValues(t, 10<<20, spec.MaxBytes, "the size limit of a voice sample")

	assert.Contains(t, env.CallError("update_character_voice_reference", map[string]any{"character_id": "c1"}), "validation:")
	assert.Contains(t, env.CallError("update_character_voice_reference", map[string]any{"project_id": p.ID, "series_id": "x", "character_id": "c1"}), "validation:")
	assert.Contains(t, env.CallError("update_character_voice_reference", map[string]any{"project_id": p.ID, "character_id": "c1", "text": "x"}), "validation:", "a Character that has no voice needs a recording first")
	assert.Contains(t, env.CallError("update_character_voice_reference", map[string]any{"project_id": p.ID, "character_id": "nope", "text": "x"}), "not_found")
	assert.Contains(t, env.CallError("update_character_voice_reference", map[string]any{"project_id": p.ID, "character_id": "c1", "text": strings.Repeat("あ", 501)}), "validation:")
	assert.Contains(t, env.CallError("delete_character_voice_reference", map[string]any{"project_id": "nope", "character_id": "c1"}), "not_found")
	assert.Contains(t, env.CallError("get_character_voice_reference_url", map[string]any{"character_id": "c1"}), "validation:")
}

func uploadSpec(t *testing.T, files *mcpkit.Files) mcpkit.UploadSpec {
	t.Helper()
	for _, s := range files.UploadPurposes() {
		if s.Purpose == "character_voice_reference" {
			return s
		}
	}
	require.Fail(t, "the purpose is not registered")
	return mcpkit.UploadSpec{}
}
