package modeltrial_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/files"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modeltrial"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

// rig is the MCP environment with the model manager over fakes and an external fake ComfyUI "box" that has an image model
// "t2i", one that takes a reference picture ("img"), and video models "vid" (first frame) and "t2v".
type rig struct {
	*mcptest.Env
	mm    *mmtest.Rig
	comfy *comfyuitest.Fake
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{}
	r.Env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		r.mm = mmtest.New(t, mmtest.WithDB(env.DB))
		env.Services.ModelManager = r.mm.Svc
		jobs.Register(reg, env.Services)
		files.Register(reg, env.Services)
		modelmgr.Register(reg, env.Services)
		modeltrial.Register(reg, env.Services)
	})
	r.comfy = comfyuitest.New(t)
	require.NoError(t, r.mm.Models.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: r.comfy.URL()}))
	for name, tpl := range map[string]string{"t2i": "txt2img-sdxl", "img": "img2img-sdxl", "vid": "img2video-wan22-5b", "t2v": "txt2video-wan22-5b"} {
		tm, ok := localcfg.TemplateByID(tpl)
		require.True(t, ok)
		require.NoError(t, r.mm.Models.CreateModel(t.Context(), localmodels.ModelInput{Endpoint: "box", Name: name, Model: tm.Model}))
	}
	return r
}

// wait polls get_job until the job is over.
func (r *rig) wait(id string) mcpkit.JobStatus {
	r.T.Helper()
	r.mm.Start()
	var st mcpkit.JobStatus
	require.Eventually(r.T, func() bool {
		r.CallOK("get_job", map[string]any{"job_id": id}, &st)
		return st.State.Final()
	}, 30*time.Second, 20*time.Millisecond)
	return st
}

// serve sends a request to the file endpoint (the tickets of the tools).
func (r *rig) serve(method, url string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url[strings.Index(url, mcpkit.FilesPath):], body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	r.Registrar.Files.Handler().ServeHTTP(rec, req)
	return rec
}

func multipartBody(t *testing.T, field, name string, data []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, name)
	require.NoError(t, err)
	_, _ = part.Write(data)
	require.NoError(t, w.Close())
	return &buf, w.FormDataContentType()
}

func TestListTrialModels_OffersTheModelsWithTheirCapabilities(t *testing.T) {
	r := newRig(t)
	var out modeltrial.ListModelsOutput
	r.CallOK("list_trial_models", map[string]any{}, &out)
	byID := map[string]int{}
	for i, m := range out.Models {
		byID[m.ID] = i
	}
	require.Contains(t, byID, "comfyui:box/vid")
	vid := out.Models[byID["comfyui:box/vid"]]
	assert.Equal(t, "video", string(vid.Media))
	assert.NotEmpty(t, vid.Resolutions)
	assert.NotEmpty(t, vid.Durations)
	assert.True(t, vid.AcceptsReference, "the first frame")
	assert.False(t, out.Models[byID["comfyui:box/t2i"]].AcceptsReference)
	assert.True(t, out.Models[byID["comfyui:box/img"]].AcceptsReference)
}

func TestTrial_RunsAsAJobReadsThroughGetJobAndTheFileComesAsATokenURL(t *testing.T) {
	r := newRig(t)

	// the reference picture goes up by ticket, not as base64
	var ticket mcpkit.UploadTicket
	r.CallOK("create_upload_ticket", map[string]any{"purpose": modeltrial.PurposeTrialReference}, &ticket)
	body, ct := multipartBody(t, ticket.FileField, "ref.png", comfyuitest.PNG)
	up := r.serve(http.MethodPost, ticket.URL, body, ct)
	require.Equal(t, http.StatusCreated, up.Code, up.Body.String())
	refID := strings.Split(strings.SplitN(up.Body.String(), `"reference_id":"`, 2)[1], `"`)[0]
	require.NotEmpty(t, refID)

	var start modeltrial.StartOutput
	r.CallOK("start_model_trial", map[string]any{"model_id": "comfyui:box/img", "prompt": "a red fox", "aspect_ratio": "16:9", "reference_id": refID}, &start)
	require.NotEmpty(t, start.JobID)
	assert.Equal(t, start.JobID, start.TrialID)
	assert.Equal(t, mcpkit.JobQueued, start.State)

	st := r.wait(start.JobID)
	assert.Equal(t, "gentest", st.Kind)
	assert.Equal(t, "comfyui:box/img", st.Target)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, start.TrialID, st.Result["trial_id"])
	assert.Equal(t, "image/png", st.Result["mime_type"])
	assert.Equal(t, "image", st.Result["media"])
	assert.Equal(t, 1, r.comfy.Count("POST /upload/image"), "the reference reached the server")

	// a picture that was used cannot be used again
	assert.Contains(t, r.CallError("start_model_trial", map[string]any{"model_id": "comfyui:box/img", "prompt": "x", "reference_id": refID}), "validation")

	var got modelmanager.GentestRecord
	r.CallOK("get_model_trial", map[string]any{"trial_id": start.TrialID}, &got)
	assert.Equal(t, modelmanager.GentestSucceeded, got.Status)
	assert.Equal(t, "a red fox", got.Prompt)
	assert.True(t, got.HasReference)
	var list modeltrial.ListTrialsOutput
	r.CallOK("list_model_trials", map[string]any{}, &list)
	require.Len(t, list.Trials, 1)

	var link modeltrial.FileOutput
	r.CallOK("get_model_trial_file_url", map[string]any{"trial_id": start.TrialID}, &link)
	assert.Contains(t, link.URL, mcpkit.FilesPath, "a token URL, not bytes")
	dl := r.serve(http.MethodGet, link.URL, nil, "")
	require.Equal(t, http.StatusOK, dl.Code)
	assert.Equal(t, comfyuitest.PNG, dl.Body.Bytes())
	assert.Equal(t, "image/png", dl.Header().Get("Content-Type"))

	// nothing of a trial is a Take, a Cost Ledger row or an asset
	for _, table := range []string{"take", "cost_ledger_entry", "asset"} {
		var n int
		require.NoError(t, r.DB.Reader().QueryRow("SELECT COUNT(*) FROM "+table).Scan(&n))
		assert.Zero(t, n, table)
	}

	var del modeltrial.DeletedOutput
	r.CallOK("delete_model_trial", map[string]any{"trial_id": start.TrialID}, &del)
	assert.True(t, del.Deleted)
	assert.Contains(t, r.CallError("get_model_trial", map[string]any{"trial_id": start.TrialID}), "not_found")
	assert.Contains(t, r.CallError("get_model_trial_file_url", map[string]any{"trial_id": start.TrialID}), "not_found")
	assert.Contains(t, r.CallError("delete_model_trial", map[string]any{"trial_id": start.TrialID}), "not_found")
}

func TestTrial_AVideoTrialAndAFailedTrial(t *testing.T) {
	r := newRig(t)
	var vid modeltrial.StartOutput
	r.CallOK("start_model_trial", map[string]any{"model_id": "comfyui:box/t2v", "prompt": "waves"}, &vid)
	st := r.wait(vid.JobID)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "video", st.Result["media"])
	assert.Equal(t, "video/mp4", st.Result["mime_type"])

	r.comfy.FailWith = "CUDA out of memory"
	var bad modeltrial.StartOutput
	r.CallOK("start_model_trial", map[string]any{"model_id": "comfyui:box/t2i", "prompt": "x"}, &bad)
	failed := r.wait(bad.JobID)
	assert.Equal(t, mcpkit.JobFailed, failed.State)
	assert.Contains(t, failed.Error, "CUDA out of memory")
	assert.Contains(t, r.CallError("get_model_trial_file_url", map[string]any{"trial_id": bad.TrialID}), "not_found", "a failed trial has no file")
	var rec modelmanager.GentestRecord
	r.CallOK("get_model_trial", map[string]any{"trial_id": bad.TrialID}, &rec)
	assert.Equal(t, modelmanager.GentestFailed, rec.Status)
}

func TestTrial_RefusalsBeforeAnythingIsQueued(t *testing.T) {
	r := newRig(t)
	for name, args := range map[string]map[string]any{
		"no prompt":        {"model_id": "comfyui:box/t2i", "prompt": " "},
		"bad model id":     {"model_id": "sdxl", "prompt": "x"},
		"image duration":   {"model_id": "comfyui:box/t2i", "prompt": "x", "duration_seconds": 3},
		"video length":     {"model_id": "comfyui:box/t2v", "prompt": "x", "duration_seconds": 99},
		"bad aspect":       {"model_id": "comfyui:box/t2i", "prompt": "x", "aspect_ratio": "wide"},
		"reference needed": {"model_id": "comfyui:box/t2i", "prompt": "x", "reference_id": "0123456789abcdef.png"},
		"no such picture":  {"model_id": "comfyui:box/img", "prompt": "x", "reference_id": "0123456789abcdef.png"},
		"bad picture id":   {"model_id": "comfyui:box/img", "prompt": "x", "reference_id": "../../etc/passwd"},
	} {
		assert.Contains(t, r.CallError("start_model_trial", args), "validation", name)
	}
	assert.Contains(t, r.CallError("start_model_trial", map[string]any{"model_id": "comfyui:box/missing", "prompt": "x"}), "not_found")
	var list modeltrial.ListTrialsOutput
	r.CallOK("list_model_trials", map[string]any{}, &list)
	assert.Empty(t, list.Trials)
	assert.Zero(t, r.comfy.Count("POST /prompt"))
}

func TestTrial_ReferenceUploadIsCheckedAsAPicture(t *testing.T) {
	r := newRig(t)
	var ticket mcpkit.UploadTicket
	r.CallOK("create_upload_ticket", map[string]any{"purpose": modeltrial.PurposeTrialReference}, &ticket)
	assert.Equal(t, int64(20<<20), ticket.MaxBytes)
	body, ct := multipartBody(t, ticket.FileField, "ref.png", []byte("GIF89a not what it says"))
	rec := r.serve(http.MethodPost, ticket.URL, body, ct)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "PNG・JPEG・WebP")
}

func TestTools_UnavailableWithoutAModelManager(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { modeltrial.Register(r, env.Services) })
	assert.Contains(t, env.CallError("list_model_trials", map[string]any{}), "unavailable")
	assert.Contains(t, env.CallError("start_model_trial", map[string]any{"model_id": "comfyui:a/b", "prompt": "x"}), "unavailable")
}

// A job that the registry never saw (started from the screen, or before a restart) is read by the id of its row: the Probe
// of the kind answers, so get_job works for the jobs of the model manager whoever started them.
func TestGetJob_ReadsAJobStartedElsewhere(t *testing.T) {
	r := newRig(t)
	ref, err := r.mm.Svc.StartGentest(t.Context(), modelmanager.GentestInput{ModelID: "comfyui:box/t2i", Prompt: "from the screen"})
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	assert.Equal(t, "gentest", st.Kind)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, ref.JobID, st.Result["trial_id"])

	inst, err := r.mm.Svc.InstallEngine(t.Context(), modelmanager.EngineInstallInput{Variant: "nvidia"})
	require.NoError(t, err)
	est := r.wait(inst.JobID)
	assert.Equal(t, "model_install", est.Kind)
	assert.Equal(t, mcpkit.JobSucceeded, est.State, est.Error)
	r.CallOK("get_job", map[string]any{"job_id": inst.JobID}, &est) // once more: the progress of a job that ended is 100 %
	assert.Equal(t, 100, est.Done)
}
