package editor_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/editor"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

func TestVoiceSampleUpload_ClonesTheVoiceOfTheSpeaker(t *testing.T) {
	f := newFixture(t)
	sample := testenv.FFmpegFile(t, ".wav", "-f", "lavfi", "-i", "sine=frequency=440:duration=1")
	params := map[string]string{"project_id": f.ProjectID, "speaker": "ユイ"}

	rec := f.Upload("voice_sample", params, "sample.wav", sample)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "cloned-voice-1", "the vendor's voice id is not handed out")

	var out editor.VoicesOutput
	f.CallOK("list_voice_clones", map[string]any{"project_id": f.ProjectID}, &out)
	require.Len(t, out.Voices, 1)
	assert.Equal(t, "ユイ", out.Voices[0].Speaker)
	assert.Equal(t, "ユイ", out.Voices[0].Name, "the name defaults to the speaker")

	// A speaker has one voice: a new clone replaces the old one.
	params["name"] = "ユイ（新）"
	require.Equal(t, http.StatusCreated, f.Upload("voice_sample", params, "sample.wav", sample).Code)
	f.CallOK("list_voice_clones", map[string]any{"project_id": f.ProjectID}, &out)
	require.Len(t, out.Voices, 1)
	assert.Equal(t, "ユイ（新）", out.Voices[0].Name)

	var view editor.ViewOutput
	f.CallOK("get_editor_view", map[string]any{"project_id": f.ProjectID}, &view)
	assert.Len(t, view.Voices, 1, "the editor view lists the same voices")
}

func TestVoiceSampleUpload_RefusesWhatTheScreenRefuses(t *testing.T) {
	f := newFixture(t)
	sample := []byte("RIFF....WAVE")

	_, err := f.Registrar.Files.UploadTicketFor(t.Context(), "voice_sample", map[string]string{"project_id": f.ProjectID})
	require.Error(t, err, "a speaker is required")

	long := make([]rune, 200)
	for i := range long {
		long[i] = 'あ'
	}
	rec := f.Upload("voice_sample", map[string]string{"project_id": f.ProjectID, "speaker": string(long)}, "s.wav", sample)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "a speaker name that is too long")

	rec = f.Upload("voice_sample", map[string]string{"project_id": "nope", "speaker": "ユイ"}, "s.wav", sample)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	rec = f.Upload("voice_sample", map[string]string{"project_id": f.ProjectID, "speaker": "ユイ"}, "s.wav", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "an empty sample")

	spec := uploadSpec(t, f.Registrar.Files, "voice_sample")
	assert.EqualValues(t, service.MaxVoiceSampleBytes, spec.MaxBytes)

	var out editor.VoicesOutput
	f.CallOK("list_voice_clones", map[string]any{"project_id": f.ProjectID}, &out)
	assert.Empty(t, out.Voices, "no refused upload made a voice")
	assert.NotNil(t, out.Voices)
	assert.Contains(t, f.CallError("list_voice_clones", map[string]any{"project_id": "nope"}), "not_found")
}

func uploadSpec(t *testing.T, files *mcpkit.Files, purpose string) mcpkit.UploadSpec {
	t.Helper()
	for _, s := range files.UploadPurposes() {
		if s.Purpose == purpose {
			return s
		}
	}
	require.Failf(t, "purpose not registered", "%s", purpose)
	return mcpkit.UploadSpec{}
}

func TestGetEditPreviewURL_ServesTheRenderedRoughCut(t *testing.T) {
	f := newFixture(t)
	id := roughCut(t, f).Timeline.ID

	var link mcpkit.Link
	f.CallOK("get_edit_preview_url", map[string]any{"timeline_id": id}, &link)
	assert.Contains(t, link.FileName, id)
	rec := f.Download(link.URL)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "video/mp4", rec.Header().Get("Content-Type"))
	assert.Greater(t, rec.Body.Len(), 1000)

	assert.Contains(t, f.CallError("get_edit_preview_url", map[string]any{"timeline_id": "nope"}), "not_found")
}

func TestGetExportDownloadURL_UnknownExportIsNotFound(t *testing.T) {
	f := newFixture(t)
	assert.Contains(t, f.CallError("get_export_download_url", map[string]any{"project_id": f.ProjectID, "export_id": "nope"}), "not_found")
	// An export of another Project is not reachable through this one.
	f.Exec(`INSERT INTO export (id, project_id, file_path, resolution, aspect_ratio, codec) VALUES ('e1', ?, 'x/e1.mp4', '1080p', '16:9', 'h264')`, f.ProjectID)
	assert.Contains(t, f.CallError("get_export_download_url", map[string]any{"project_id": "other", "export_id": "e1"}), "not_found")
	assert.Contains(t, f.CallError("get_export_download_url", map[string]any{"project_id": f.ProjectID, "export_id": "e1"}), "not_found", "the file is gone")

}
