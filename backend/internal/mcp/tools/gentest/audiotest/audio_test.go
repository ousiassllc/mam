// Package audiotest_test tests the audio input of the MCP tools of the generation test (Issue #1153): the upload purpose gentest_audio, and a
// speech-to-video model that cannot run without its sound.
package audiotest_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

type rig struct {
	*mcptest.Env
	GT *gentesttest.Rig
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{}
	r.Env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		r.GT = gentesttest.NewOn(t, env.DB, gentesttest.WithInputModels())
		env.Services.Gentest = r.GT.Svc
		jobs.Register(reg, env.Services)
		files.Register(reg, env.Services)
		mcpgentest.Register(reg, env.Services)
	})
	return r
}

// upload sends data as the file of a new gentest_audio ticket through the file endpoint, the way a client does.
func (r *rig) upload(name string, data []byte) *httptest.ResponseRecorder {
	r.T.Helper()
	var ticket mcpkit.UploadTicket
	r.CallOK("create_upload_ticket", map[string]any{"purpose": mcpgentest.PurposeAudio}, &ticket)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile(ticket.FileField, name)
	require.NoError(r.T, err)
	_, _ = part.Write(data)
	require.NoError(r.T, w.Close())
	req := httptest.NewRequest(http.MethodPost, ticket.URL[strings.Index(ticket.URL, mcpkit.FilesPath):], &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	r.Registrar.Files.Handler().ServeHTTP(rec, req)
	return rec
}

// uploaded is the audio_id of an accepted upload and the type it was stored as.
func (r *rig) uploaded(name string, data []byte) (id, mime string) {
	r.T.Helper()
	rec := r.upload(name, data)
	require.Equal(r.T, http.StatusCreated, rec.Code, rec.Body.String())
	var got struct {
		Result struct {
			AudioID  string `json:"audio_id"`
			MimeType string `json:"mime_type"`
		} `json:"result"`
	}
	require.NoError(r.T, json.Unmarshal(rec.Body.Bytes(), &got))
	return got.Result.AudioID, got.Result.MimeType
}

// s2vBatch is a batch for model with the reference picture and, when audioID is not empty, the uploaded audio.
func (r *rig) s2vBatch(model domain.ModelName, audioID string) map[string]any {
	r.T.Helper()
	pic, _, err := r.GT.Svc.StageReference(gentesttest.PNG)
	require.NoError(r.T, err)
	refs := []map[string]any{{"source": "upload", "id": pic, "role": "reference"}}
	if audioID != "" {
		refs = append(refs, map[string]any{"source": "upload", "id": audioID, "role": "audio"})
	}
	return map[string]any{"media": "video", "models": []string{string(model)}, "prompts": []map[string]any{{"prompt": "a singer"}}, "references": refs}
}

func TestUploadStagesEveryKindByItsBytesAndRefusesAnythingElse(t *testing.T) {
	r := newRig(t)
	var ticket mcpkit.UploadTicket
	r.CallOK("create_upload_ticket", map[string]any{"purpose": mcpgentest.PurposeAudio}, &ticket)
	assert.Equal(t, int64(audiofile.MaxBytes), ticket.MaxBytes)

	id, mime := r.uploaded("voice.wav", gentesttest.WAV)
	assert.Equal(t, audiofile.WAV, mime)
	assert.True(t, strings.HasSuffix(id, ".wav"), id)
	id, mime = r.uploaded("voice.mp3", []byte("ID3\x04\x00\x00\x00\x00\x00\x00 an mp3 tag"))
	assert.Equal(t, audiofile.MP3, mime)
	assert.True(t, strings.HasSuffix(id, ".mp3"), id)
	for ext, c := range map[string]struct {
		data []byte
		mime string
	}{
		".flac": {audiofiletest.FLAC(44100, 44100), audiofile.FLAC},
		".ogg":  {audiofiletest.OGGVorbis(44100, 44100), audiofile.OGG},
		".m4a":  {audiofiletest.M4A(0, 44100, 44100, true), audiofile.M4A},
	} {
		id, mime = r.uploaded("voice"+ext, c.data)
		assert.Equal(t, c.mime, mime, ext)
		assert.True(t, strings.HasSuffix(id, ext), id)
	}
	id, mime = r.uploaded("lying.png", gentesttest.WAV)
	assert.Equal(t, audiofile.WAV, mime, "the kind is read from the bytes, not from the name")
	assert.True(t, strings.HasSuffix(id, ".wav"), id)

	oversized := make([]byte, audiofile.MaxBytes+1)
	copy(oversized, gentesttest.WAV)
	for name, file := range map[string][]byte{
		"a picture named like a wav": gentesttest.PNG,
		"a text":                     []byte("just some words"),
		"a WAV header that is cut":   []byte("RIFF\x00\x00\x00"),
		"an empty file":              {},
		"a WAV one byte over limit":  oversized,
		"an mp4 video (isom)":        audiofiletest.MP4WithBrand("isom", 1000, 5000, "vide"),
	} {
		rec := r.upload("voice.wav", file)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name+": "+rec.Body.String())
	}
}

func TestEstimateAnS2VBatchNeedsItsAudioAndAnUnusedAudioIsStopped(t *testing.T) {
	r := newRig(t)
	audioID, _ := r.uploaded("voice.wav", gentesttest.WAV)

	var est gentest.Estimate
	r.CallOK("estimate_generation_test", r.s2vBatch(gentesttest.S2VModel, audioID), &est)
	assert.Equal(t, 1, est.TotalItems)

	for _, tool := range []string{"estimate_generation_test", "run_generation_test"} {
		args := r.s2vBatch(gentesttest.S2VModel, "")
		if tool == "run_generation_test" {
			args["estimate_id"], args["confirmed_total_usd"] = "x", 0
		}
		text := r.CallError(tool, args)
		assert.Contains(t, text, "validation", tool)
		assert.Contains(t, text, `"code":"input_missing"`, tool)
		assert.Contains(t, text, "音声ファイルが必要です", tool)
		assert.Contains(t, text, "needs audio", tool)
	}

	t2v := r.s2vBatch(gentesttest.T2VModel, audioID)
	t2v["references"] = t2v["references"].([]map[string]any)[1:]
	text := r.CallError("estimate_generation_test", t2v)
	assert.Contains(t, text, `"code":"input_unused"`)
	assert.Contains(t, text, "input roles: audio", "the refusal names the audio as what nothing uses")
	assert.Empty(t, r.GT.Fake.Calls(), "nothing was sent to a model")
}

func TestRunAnS2VBatchHandsTheUploadedAudioToTheModelAndRecordsIt(t *testing.T) {
	r := newRig(t)
	audioID, _ := r.uploaded("voice.wav", gentesttest.WAV)
	args := r.s2vBatch(gentesttest.S2VModel, audioID)

	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	args["estimate_id"], args["confirmed_total_usd"] = est.ID, est.TotalUSD
	var out mcpgentest.RunOutput
	r.CallOK("run_generation_test", args, &out)
	require.NotEmpty(t, out.JobID)
	require.NotEmpty(t, out.BatchID)
	r.WaitJob(out.JobID, 20*time.Second, 10*time.Millisecond)

	calls := r.GT.Fake.CallsOf(gentesttest.S2VModel)
	require.Len(t, calls, 1)
	require.NotNil(t, calls[0].Video)
	require.NotNil(t, calls[0].Video.Audio, "the audio reaches the model as its own input")
	assert.Equal(t, gentesttest.WAV, calls[0].Video.Audio.Data)
	assert.Equal(t, audiofile.WAV, calls[0].Video.Audio.MimeType)
	require.Len(t, calls[0].Video.References, 1, "the picture is the reference, the audio is not one of the pictures")
	assert.Equal(t, gentesttest.PNG, calls[0].Video.References[0].Data)

	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	require.Len(t, b.Results, 1)
	assert.Equal(t, "succeeded", b.Results[0].Status)
	var got mcpgentest.ResultOutput
	r.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &got)
	assert.Empty(t, got.UnusedInputs)
	roles := map[string]string{}
	for _, in := range got.Inputs {
		roles[in.Role] = in.MimeType
	}
	assert.Equal(t, map[string]string{"reference": "image/png", "audio": audiofile.WAV}, roles, "the audio is kept with the batch as an audio input")
}

func TestListModelsSaysAnS2VModelNeedsAudio(t *testing.T) {
	r := newRig(t)
	var out mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"media": "video"}, &out)
	by := map[string]modelcatalog.Info{}
	for _, m := range out.Models {
		by[m.ID] = m
	}
	s2v := by[string(gentesttest.S2VModel)]
	assert.Contains(t, s2v.RequiresInputs, "audio")
	require.NotNil(t, s2v.Video)
	assert.True(t, s2v.Video.AudioInput)
	assert.NotContains(t, by[string(gentesttest.T2VModel)].RequiresInputs, "audio")
	require.NotNil(t, by[string(gentesttest.T2VModel)].Video)
	assert.False(t, by[string(gentesttest.T2VModel)].Video.AudioInput)
}
