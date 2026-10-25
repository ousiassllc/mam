package localmodels_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// assignmentForm is the form of the "画像・動画モデル" section in body as the browser would submit it, with
// one field set.
func assignmentForm(t *testing.T, body, field, value string) url.Values {
	t.Helper()
	v := formOf(t, body, "model-assignment")
	v.Set(field, value)
	return v
}

// startWith opens the first step of a ComfyUI model with a template and returns the model form's HTML.
func startWith(t *testing.T, s *screen, endpoint, template string) string {
	t.Helper()
	rec := s.do(http.MethodPost, paths.LocalEndpoint(endpoint, "models", "start"), url.Values{"template": {template}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

func TestModel_TemplateFormSubmitsAsItIsAndTheModelAppearsInTheAssignments(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188").Code)

	first := s.do(http.MethodGet, paths.LocalEndpoint("box", "models", "new"), nil)
	require.Equal(t, http.StatusOK, first.Code)
	assert.Contains(t, first.Body.String(), `data-testid="local-model-start"`)
	assert.Contains(t, first.Body.String(), "txt2img-sdxl", "the built-in templates are offered")
	assert.Contains(t, first.Body.String(), "txt2video-wan22-5b")

	// Image model from the template: the form the template fills submits as it is, with the name added.
	img := startWith(t, s, "box", "txt2img-sdxl")
	assert.Contains(t, img, `data-testid="local-assignments"`)
	assert.Contains(t, options(img, "assign_prompt"), "6|text", "the node inputs of the graph are the choices")
	form := formOf(t, img, "local-model-form")
	form.Set("name", "sdxl")
	rec := s.do(http.MethodPost, paths.LocalEndpoint("box", "models"), form)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-model-id="comfyui:box/sdxl"`)
	assert.Contains(t, rec.Body.String(), "モデル「comfyui:box/sdxl」を保存しました")

	// Video model from the template.
	vid := startWith(t, s, "box", "txt2video-wan22-5b")
	assert.Contains(t, vid, `data-testid="local-video-fields"`)
	form = formOf(t, vid, "local-model-form")
	form.Set("name", "wan")
	form.Set("durations", "3, 5")
	form.Set("resolutions", "480p")
	form.Set("cost_per_second_usd", "0.02")
	rec = s.do(http.MethodPost, paths.LocalEndpoint("box", "models"), form)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-model-id="comfyui:box/wan"`)
	assert.Contains(t, body, "長さ 3、5 秒")
	assert.Contains(t, body, "見積 $0.02/秒")

	// The assignment form comes along out of band, so the new models are choices at once ...
	assert.Contains(t, body, `hx-swap-oob="true"`)
	assert.Contains(t, options(body, "image_new_reference_primary_model"), "comfyui:box/sdxl")
	assert.Contains(t, options(body, "policy_fast_primary"), "comfyui:box/wan@480p")
	assert.NotContains(t, options(body, "image_new_reference_primary_model"), "comfyui:box/wan", "a video model is not an image choice")

	// ... and so on the screen after a reload.
	page := s.modelsPage()
	for _, purpose := range []string{"new_reference", "edit_reference", "preview", "unified_openai"} {
		for _, slot := range []string{"primary", "fallback"} {
			assert.Contains(t, options(page, "image_"+purpose+"_"+slot+"_model"), "comfyui:box/sdxl", purpose+" "+slot)
		}
	}
	for _, policy := range []string{"fast", "balanced", "max_quality"} {
		for _, slot := range []string{"primary", "fallback"} {
			assert.Contains(t, options(page, "policy_"+policy+"_"+slot), "comfyui:box/wan@480p", policy+" "+slot)
		}
	}

	// Choosing them saves, and the model is then in use and cannot be deleted.
	form = formOf(t, page, "model-assignment")
	form.Set("image_new_reference_primary_model", "comfyui:box/sdxl")
	form.Set("policy_fast_primary", "comfyui:box/wan@480p")
	assign := s.do(http.MethodPut, paths.ModelSettings(), form)
	require.Equal(t, http.StatusOK, assign.Code, assign.Body.String())
	assert.Contains(t, assign.Body.String(), "保存しました。すぐに反映されます")
	assert.Contains(t, s.page(), "使用中: images.new_reference.primary")
	refused := s.do(http.MethodDelete, paths.LocalEndpoint("box", "models", "sdxl"), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, refused.Code)
	assert.Contains(t, refused.Body.String(), "images.new_reference.primary")
	assert.Contains(t, refused.Body.String(), "先に割り当てを変えてください")
}

func TestModel_ImportedWorkflowIsAssignedByTheTableAndReferencesAreNumberedWithoutGaps(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188").Code)
	const graph = `{"3":{"class_type":"KSampler","inputs":{"seed":1,"model":["4",0]}},
	"4":{"class_type":"CheckpointLoaderSimple","inputs":{"ckpt_name":"m.safetensors"}},
	"6":{"class_type":"CLIPTextEncode","inputs":{"text":"hi","clip":["4",1]}},
	"9":{"class_type":"LoadImage","inputs":{"image":"a.png"}},
	"10":{"class_type":"LoadImage","inputs":{"image":"b.png"}},
	"11":{"class_type":"SaveImage","inputs":{"images":["3",0],"filename_prefix":"mam"}}}`

	start := s.do(http.MethodPost, paths.LocalEndpoint("box", "models", "start"), url.Values{"template": {""}, "media": {"image"}, "workflow_json": {graph}})
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	body := start.Body.String()
	choices := options(body, "assign_prompt")
	assert.Contains(t, choices, "6|text")
	assert.NotContains(t, choices, "3|model", "an input another node feeds cannot take a value")
	assert.Contains(t, choices, "3|seed")
	assert.Equal(t, []string{"assign_reference_image_0", "assign_reference_image_1", "assign_reference_image_2", "assign_reference_image_3"},
		referenceFields(fieldNames(t, body, "local-model-form")))

	form := formOf(t, body, "local-model-form")
	form.Set("name", "mine")
	form.Set("assign_prompt", "6|text")
	form.Set("assign_seed", "3|seed")
	form.Set("assign_reference_image_1", "9|image") // slot 0 left empty
	form.Set("assign_reference_image_3", "10|image")
	form.Set("output_node", "11")
	rec := s.do(http.MethodPost, paths.LocalEndpoint("box", "models"), form)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "参照画像を使える")
	assert.Contains(t, s.savedFile(), `"index": 1`, "the two references are numbered 0 and 1")

	edit := s.do(http.MethodGet, paths.LocalEndpoint("box", "models", "mine", "edit"), nil)
	require.Equal(t, http.StatusOK, edit.Code)
	ef := formOf(t, edit.Body.String(), "local-model-form")
	assert.Equal(t, "6|text", ef.Get("assign_prompt"))
	assert.Equal(t, "9|image", ef.Get("assign_reference_image_0"))
	assert.Equal(t, "10|image", ef.Get("assign_reference_image_1"))
	assert.Equal(t, "11", ef.Get("output_node"))
	assert.NotContains(t, fieldNames(t, edit.Body.String(), "local-model-form"), "name", "the name of a model is not a field of an edit")
	assert.Contains(t, edit.Body.String(), `data-testid="local-model-fixed"`)
}

func TestModel_RefusalsKeepTheFormAndWriteNothing(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188").Code)
	before := s.savedFile()

	editorFormat := s.do(http.MethodPost, paths.LocalEndpoint("box", "models", "start"), url.Values{"template": {""}, "media": {"image"}, "workflow_json": {`{"nodes":[],"links":[]}`}})
	assert.Equal(t, http.StatusUnprocessableEntity, editorFormat.Code)
	assert.Contains(t, editorFormat.Body.String(), "Save (API Format)")
	assert.Contains(t, editorFormat.Body.String(), `data-testid="local-model-start"`)
	assert.Equal(t, http.StatusUnprocessableEntity, s.do(http.MethodPost, paths.LocalEndpoint("box", "models", "start"), url.Values{"template": {""}, "media": {"image"}, "workflow_json": {"not json"}}).Code)
	assert.Equal(t, http.StatusUnprocessableEntity, s.do(http.MethodPost, paths.LocalEndpoint("box", "models", "start"), url.Values{"template": {"nope"}}).Code)

	// No prompt assignment and no output node: refused by the service with its message, the form stays.
	form := formOf(t, startWith(t, s, "box", "txt2img-sdxl"), "local-model-form")
	form.Set("name", "bad")
	form.Set("assign_prompt", "")
	form.Set("output_node", "")
	rec := s.do(http.MethodPost, paths.LocalEndpoint("box", "models"), form)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="local-form-error"`)
	assert.Contains(t, rec.Body.String(), `data-testid="local-model-form"`)
	assert.Equal(t, before, s.savedFile())

	form.Set("assign_prompt", "6|text")
	form.Set("output_node", "9")
	form.Set("name", "Bad Name")
	assert.Equal(t, http.StatusUnprocessableEntity, s.do(http.MethodPost, paths.LocalEndpoint("box", "models"), form).Code)

	good := formOf(t, startWith(t, s, "box", "txt2img-sdxl"), "local-model-form")
	good.Set("name", "ok")
	require.Equal(t, http.StatusOK, s.do(http.MethodPost, paths.LocalEndpoint("box", "models"), good).Code)
	dup := s.do(http.MethodPost, paths.LocalEndpoint("box", "models"), good)
	assert.Equal(t, http.StatusConflict, dup.Code)
	assert.Contains(t, dup.Body.String(), "その名前のモデルは、この接続先にすでにあります")

	// An update that carries another name changes the model it was sent to, never the name.
	good.Set("name", "other")
	good.Set("cost_per_image_usd", "0.5")
	up := s.do(http.MethodPut, paths.LocalEndpoint("box", "models", "ok"), good)
	require.Equal(t, http.StatusOK, up.Code, up.Body.String())
	assert.Contains(t, up.Body.String(), `data-model-id="comfyui:box/ok"`)
	assert.NotContains(t, up.Body.String(), "comfyui:box/other")
	assert.Contains(t, up.Body.String(), "見積 $0.5/枚")
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodPut, paths.LocalEndpoint("box", "models", "nope"), good).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodGet, paths.LocalEndpoint("box", "models", "nope", "edit"), nil).Code)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodPost, paths.LocalEndpoint("nope", "models", "start"), url.Values{"template": {"txt2img-sdxl"}}).Code)

	del := s.do(http.MethodDelete, paths.LocalEndpoint("box", "models", "ok"), nil)
	require.Equal(t, http.StatusOK, del.Code)
	assert.NotContains(t, del.Body.String(), `data-model-id="comfyui:box/ok"`)
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, paths.LocalEndpoint("box", "models", "ok"), nil).Code)
}

func TestModel_OpenAICompatibleFormHasModelIDAndNoWorkflow(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("lm", "openai-compat", "http://127.0.0.1:1234").Code)

	first := s.do(http.MethodGet, paths.LocalEndpoint("lm", "models", "new"), nil)
	require.Equal(t, http.StatusOK, first.Code)
	assert.Contains(t, first.Body.String(), `data-testid="local-model-form"`, "no workflow step for this kind")
	assert.NotContains(t, first.Body.String(), `data-testid="local-assignments"`)
	assert.Equal(t, http.StatusUnprocessableEntity, s.do(http.MethodPost, paths.LocalEndpoint("lm", "models", "start"), url.Values{"template": {"txt2img-sdxl"}}).Code)

	rec := s.do(http.MethodPost, paths.LocalEndpoint("lm", "models"), url.Values{
		"name": {"flux"}, "media": {"video"}, "model_id": {"flux-schnell"}, "accepts_image_inputs": {"1"}, "cost_per_image_usd": {"0.01"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-model-id="openai-compat:lm/flux"`)
	assert.Contains(t, rec.Body.String(), "モデル ID: flux-schnell")
	assert.Contains(t, rec.Body.String(), "参照画像を使える")
	assert.Contains(t, options(rec.Body.String(), "image_preview_primary_model"), "openai-compat:lm/flux", "an OpenAI-compatible endpoint serves images only, whatever the form says")

	missing := s.do(http.MethodPost, paths.LocalEndpoint("lm", "models"), url.Values{"name": {"empty"}, "media": {"image"}, "model_id": {""}})
	assert.Equal(t, http.StatusUnprocessableEntity, missing.Code)
}

func TestPinned_TheSectionIsReadOnlyButKeysAndTestsWork(t *testing.T) {
	const content = `{"endpoints":{"box":{"kind":"openai-compat","base_url":"http://127.0.0.1:1234"}},
	"local_models":{"box/flux":{"media":"image","model":"flux"}}}`
	s := newScreen(t, true, content)
	body := s.page()

	assert.Contains(t, body, `data-testid="local-models-pinned"`)
	assert.Contains(t, body, "--model-config")
	assert.Contains(t, body, `data-endpoint="box"`)
	assert.Contains(t, body, `data-model-id="openai-compat:box/flux"`, "the pinned file's models are listed")
	panel := body[strings.Index(body, `id="local-models-panel"`):]
	panel = panel[:strings.Index(panel, `data-testid="local-endpoint-key"`)+1]
	assert.NotContains(t, panel, `hx-delete="`+paths.LocalEndpoint("box")+`"`, "the delete button is disabled")
	assert.NotContains(t, body, `hx-get="`+paths.LocalEndpoints()+`/new"`, "the add button is disabled")
	assert.Contains(t, body, "disabled")
	assert.Contains(t, body, `hx-post="`+paths.LocalEndpoint("box", "test")+`"`, "the connection test still works")

	for _, req := range []struct {
		method, path string
		form         url.Values
	}{
		{http.MethodPost, paths.LocalEndpoints(), url.Values{"name": {"x"}, "kind": {"comfyui"}, "base_url": {"http://127.0.0.1:8188"}}},
		{http.MethodPut, paths.LocalEndpoint("box"), url.Values{"base_url": {"http://127.0.0.1:9"}}},
		{http.MethodDelete, paths.LocalEndpoint("box"), nil},
		{http.MethodPost, paths.LocalEndpoint("box", "models"), url.Values{"name": {"m"}, "media": {"image"}, "model_id": {"x"}}},
		{http.MethodDelete, paths.LocalEndpoint("box", "models", "flux"), nil},
	} {
		rec := s.do(req.method, req.path, req.form)
		assert.Equal(t, http.StatusConflict, rec.Code, req.method+" "+req.path)
		assert.Contains(t, rec.Body.String(), "起動オプションで固定されているため、この画面では変更できません")
	}
	assert.Equal(t, content, s.savedFile(), "the pinned file is never written")

	opened := s.do(http.MethodGet, paths.LocalEndpoints()+"/new", nil)
	assert.Equal(t, http.StatusConflict, opened.Code, "a form that could not be saved is not opened")
	assert.Equal(t, "#local-models-panel", opened.Header().Get("HX-Retarget"))

	key := s.do(http.MethodPut, paths.LocalEndpoint("box", "key"), url.Values{"api_key": {"pinned-key-5678"}})
	assert.Equal(t, http.StatusOK, key.Code)
	assert.Contains(t, key.Body.String(), "設定済み（末尾 678）")
	assert.Equal(t, content, s.savedFile())
}

func TestProblem_AnUnreadableFileIsOnlyThisSectionsProblem(t *testing.T) {
	s := newScreen(t, false, "")
	require.NoError(t, os.WriteFile(s.file, []byte("{broken"), 0o600))
	body := s.page()
	assert.Contains(t, body, `data-testid="local-models-problem"`)
	assert.Contains(t, body, `<nav aria-label="設定"`, "the rest of the screen works")
}

func referenceFields(names []string) []string {
	var out []string
	for i := range 4 {
		for _, n := range names {
			if n == "assign_reference_image_"+string(rune('0'+i)) {
				out = append(out, n)
			}
		}
	}
	return out
}
