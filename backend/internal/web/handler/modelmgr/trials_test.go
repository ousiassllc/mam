package modelmgr_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// externalBox registers the fake ComfyUI as the endpoint "box" with an image model "t2i", one that needs a reference image
// ("img") and a video model "vid".
func (s *screen) externalBox() *comfyuitest.Fake {
	s.t.Helper()
	f := comfyuitest.New(s.t)
	require.NoError(s.t, s.rig.Models.CreateEndpoint(s.t.Context(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	for name, tpl := range map[string]string{"img": "img2img-sdxl", "vid": "img2video-wan22-5b", "t2i": "txt2img-sdxl"} {
		tm, ok := localcfg.TemplateByID(tpl)
		require.True(s.t, ok)
		require.NoError(s.t, s.rig.Models.CreateModel(s.t.Context(), localmodels.ModelInput{Endpoint: "box", Name: name, Model: tm.Model}))
	}
	return f
}

// multipartTrial sends the trial form as the browser does (multipart, with an optional picture).
func (s *screen) multipartTrial(fields map[string]string, picture []byte) *httptest.ResponseRecorder {
	s.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		require.NoError(s.t, w.WriteField(k, v))
	}
	if picture != nil {
		part, err := w.CreateFormFile("reference", "ref.png")
		require.NoError(s.t, err)
		_, _ = part.Write(picture)
	}
	require.NoError(s.t, w.Close())
	req := httptest.NewRequest(http.MethodPost, paths.ModelManagerPart("trials"), &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func TestTrial_NoModelShowsWhyAndAStartIsRefused(t *testing.T) {
	s := newScreen(t)
	body := s.page()
	assert.Contains(t, body, `data-testid="mm-trial-unavailable"`)
	assert.Contains(t, body, `data-testid="mm-trials-empty"`)
	rec := s.multipartTrial(map[string]string{"model_id": "comfyui:box/img", "prompt": "x"}, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// settled starts the queue and returns the trial list once no trial waits or runs.
func (s *screen) settled() string {
	s.t.Helper()
	s.rig.Start()
	var body string
	require.Eventually(s.t, func() bool {
		body = s.get(paths.ModelManagerPart("trials")).Body.String()
		return !strings.Contains(body, `hx-trigger="every 2s"`)
	}, 30*time.Second, 20*time.Millisecond, "the trial did not end")
	return body
}

// resultID reads the id of the first trial of a list from its file URL or delete button.
func resultID(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`/model-manager/trials/([^/"?]+)`).FindStringSubmatch(body)
	require.Len(t, m, 2, body)
	return m[1]
}

func TestTrial_RunsAsABatchOfTheGenerationTestAndTheResultIsServedInASandbox(t *testing.T) {
	s := newScreen(t)
	f := s.externalBox()
	form := s.get(paths.ModelManagerPart("trials", "form") + "?model_id=comfyui:box/vid").Body.String()
	assert.Contains(t, form, `name="resolution"`, "a video model offers its resolutions")
	assert.Contains(t, form, `name="duration_seconds"`)
	assert.Contains(t, form, `name="negative_prompt"`)
	assert.Contains(t, form, `name="reference"`, "the first frame")

	rec := s.multipartTrial(map[string]string{"model_id": "comfyui:box/img", "prompt": "a red fox", "aspect_ratio": "16:9"}, comfyuitest.PNG)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "試し生成を始めました")
	assert.Equal(t, 1, strings.Count(rec.Body.String(), `data-testid="mm-trial-item"`), "the result is on the list at once")
	assert.Contains(t, rec.Body.String(), `hx-trigger="every 2s"`, "the list polls while the result waits")
	assert.Contains(t, rec.Body.String(), paths.GenerationTestHistory(), "the history of the generation test keeps it")

	list := s.settled()
	assert.Contains(t, list, `data-status="succeeded"`)
	id := resultID(t, list)
	assert.Contains(t, list, `src="`+paths.ModelManagerPart("trials", id, "file")+`"`)
	assert.Equal(t, 1, f.Count("POST /upload/image"), "the reference reached the server")

	file := s.get(paths.ModelManagerPart("trials", id, "file"))
	require.Equal(t, http.StatusOK, file.Code)
	assert.Equal(t, "image/png", file.Header().Get("Content-Type"))
	assert.Equal(t, "sandbox", file.Header().Get("Content-Security-Policy"))
	assert.Equal(t, comfyuitest.PNG, file.Body.Bytes())
	assert.Equal(t, http.StatusNotFound, s.get(paths.ModelManagerPart("trials", "nope", "file")).Code)

	hist, err := s.rig.Gentest.History(t.Context(), gentest.HistoryFilter{})
	require.NoError(t, err)
	require.Len(t, hist, 1, "the trial is a result of the generation test")
	assert.Equal(t, id, hist[0].ID)

	del := s.do(http.MethodDelete, paths.ModelManagerPart("trials", id), nil)
	require.Equal(t, http.StatusOK, del.Code)
	assert.Contains(t, del.Body.String(), "試し生成の結果を削除しました")
	assert.Equal(t, http.StatusNotFound, s.get(paths.ModelManagerPart("trials", id, "file")).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, paths.ModelManagerPart("trials", id), nil).Code)
}

func TestTrial_RefusalsAreCheckedAgainstTheModelBeforeAnythingIsQueued(t *testing.T) {
	s := newScreen(t)
	s.externalBox()
	for name, fields := range map[string]map[string]string{
		"no prompt":    {"model_id": "comfyui:box/img", "prompt": "  "},
		"bad aspect":   {"model_id": "comfyui:box/img", "prompt": "x", "aspect_ratio": "wide"},
		"not a number": {"model_id": "comfyui:box/vid", "prompt": "x", "duration_seconds": "long"},
	} {
		rec := s.multipartTrial(fields, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `data-testid="mm-trial-error"`, name)
		assert.Contains(t, rec.Body.String(), `data-testid="mm-trial-form"`, name+": the form stays")
	}
	notPicture := s.multipartTrial(map[string]string{"model_id": "comfyui:box/img", "prompt": "x"}, []byte("GIF89a not what it says"))
	assert.Equal(t, http.StatusUnprocessableEntity, notPicture.Code)
	assert.Contains(t, notPicture.Body.String(), "PNG・JPEG・WebP")
	hist, err := s.rig.Gentest.History(t.Context(), gentest.HistoryFilter{})
	require.NoError(t, err)
	assert.Empty(t, hist, "nothing started")
}

func TestTrial_AFailedTrialShowsItsReason(t *testing.T) {
	s := newScreen(t)
	f := s.externalBox()
	f.FailWith = "CUDA out of memory"
	rec := s.multipartTrial(map[string]string{"model_id": "comfyui:box/t2i", "prompt": "x"}, nil)
	require.Equal(t, http.StatusAccepted, rec.Code)
	list := s.settled()
	assert.Contains(t, list, `data-status="failed"`)
	assert.Contains(t, list, "CUDA out of memory")
}

func TestHuggingFaceToken_IsStatusOnlyAndNeverRendered(t *testing.T) {
	s := newScreen(t)
	const secret = "hf_abcdefghijklmnopqrstuvwxyz1234"
	assert.Contains(t, s.page(), "トークンは設定されていません")

	rec := s.do(http.MethodPut, paths.ModelManagerPart("hf-token"), url.Values{"token": {secret}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "トークンは設定済みです（末尾 1234）")
	assert.NotContains(t, rec.Body.String(), secret)
	assert.NotContains(t, rec.Header().Get("HX-Trigger"), secret)
	assert.NotContains(t, s.page(), secret)
	stored, err := s.rig.Keys.Get("huggingface")
	require.NoError(t, err)
	assert.Equal(t, secret, stored)

	empty := s.do(http.MethodPut, paths.ModelManagerPart("hf-token"), url.Values{"token": {"   "}})
	assert.Equal(t, http.StatusUnprocessableEntity, empty.Code)
	assert.Contains(t, empty.Body.String(), "設定済みです", "the saved token stays")

	del := s.do(http.MethodDelete, paths.ModelManagerPart("hf-token"), nil)
	require.Equal(t, http.StatusOK, del.Code)
	assert.Contains(t, del.Body.String(), "トークンを削除しました")
	_, err = s.rig.Keys.Get("huggingface")
	assert.Error(t, err)
}

// fakeComfy is an external ComfyUI that has a few model files; it answers GET only.
func fakeComfy(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "read only", http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/system_stats":
			_, _ = w.Write([]byte(`{"system":{"comfyui_version":"0.42.1"}}`))
		case "/models/checkpoints":
			_, _ = w.Write([]byte(`["a.safetensors","other.ckpt"]`))
		case "/models/vae":
			_, _ = w.Write([]byte(`["shared.safetensors"]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestExternal_ListsWhatAnExternalComfyUIHasAndWhatIsMissing(t *testing.T) {
	s := newScreen(t)
	none := s.get(paths.ModelManagerPart("external")).Body.String()
	assert.Contains(t, none, `data-testid="mm-external-none"`)

	srv := fakeComfy(t)
	require.NoError(t, s.rig.Models.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: "tailnet", Kind: localcfg.KindComfyUI, BaseURL: srv.URL}))
	form := s.get(paths.ModelManagerPart("external")).Body.String()
	assert.Contains(t, form, `<option value="tailnet"`)
	got := s.get(paths.ModelManagerPart("external") + "?endpoint=tailnet")
	require.Equal(t, http.StatusOK, got.Code, got.Body.String())
	body := got.Body.String()
	assert.Contains(t, body, `data-testid="mm-external-result"`)
	assert.Contains(t, body, "ComfyUI 0.42.1")
	assert.Contains(t, body, "other.ckpt")
	assert.Contains(t, body, `data-complete="true"`, "m-a has its file there")
	assert.Contains(t, body, `data-complete="false"`)
	assert.Contains(t, body, "足りない: diffusion_models/b.safetensors")
	assert.Contains(t, body, "mam は読むだけです")
	assert.NotContains(t, body, `hx-post=`+`"/model-manager/models/`, "no install action for an external server")

	assert.Equal(t, http.StatusNotFound, s.get(paths.ModelManagerPart("external")+"?endpoint=nope").Code)
	gone := s.get(paths.ModelManagerPart("external") + "?endpoint=tailnet")
	assert.Equal(t, http.StatusOK, gone.Code)
	srv.Close()
	down := s.get(paths.ModelManagerPart("external") + "?endpoint=tailnet")
	assert.Equal(t, http.StatusBadGateway, down.Code)
	assert.Contains(t, down.Body.String(), "ComfyUI に接続できませんでした")

	s.rig.InstallEngine()
	bundled := s.get(paths.ModelManagerPart("external") + "?endpoint=bundled-comfyui")
	assert.Equal(t, http.StatusUnprocessableEntity, bundled.Code, "the bundled endpoint is not an external one")
}

func TestSettingsAndTheProjectListLinkToTheScreen(t *testing.T) {
	s := newScreen(t)
	req := httptest.NewRequest(http.MethodGet, paths.Settings(), nil)
	assert.Contains(t, s.send(req).Body.String(), `href="`+paths.ModelManager()+`"`)
	assert.Contains(t, s.send(httptest.NewRequest(http.MethodGet, paths.Home(), nil)).Body.String(), `href="`+paths.ModelManager()+`"`)
}
