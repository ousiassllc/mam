package local_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// savedWorkflow is the workflow of the model "box/name" in models.json, as JSON.
func savedWorkflow(t *testing.T, s *screen, name string) json.RawMessage {
	t.Helper()
	var file struct {
		Models map[string]struct {
			Workflow json.RawMessage `json:"workflow"`
		} `json:"local_models"`
	}
	require.NoError(t, json.Unmarshal([]byte(s.savedFile()), &file))
	m, ok := file.Models["box/"+name]
	require.True(t, ok, "no model %s in %s", name, s.savedFile())
	return m.Workflow
}

// templateJSON is the template's workflow as JSON.
func templateJSON(t *testing.T, id string) string {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok)
	raw, err := json.Marshal(tpl.Model.Workflow)
	require.NoError(t, err)
	return string(raw)
}

func referenceSlotNames(names []string) int {
	n := 0
	for _, f := range names {
		if strings.HasPrefix(f, "assign_reference_image_") {
			n++
		}
	}
	return n
}

// A bundled image model made from its template on the screen and then edited there (a price) keeps what the form has no field
// for: the reference mode, the optional flag and all ten reference slots of Qwen-Image 2.1, the LoRA node, Chroma's denoise
// floor, what a run without a reference changes and the extra width / height of its reference scale.
func TestModel_BundledImageModelKeepsItsReferenceFieldsThroughTheForms(t *testing.T) {
	for _, id := range []string{"txt2img-qwen-image21", "txt2img-chroma", "txt2img-flux-dev-lora"} {
		t.Run(id, func(t *testing.T) {
			s := newScreen(t, false, "")
			require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188").Code)
			tpl, _ := localcfg.TemplateByID(id)
			wantSlots := max(4, tpl.Model.Workflow.Count(localcfg.ParamReferenceImage))

			body := startWith(t, s, "box", id)
			assert.Equal(t, wantSlots, referenceSlotNames(fieldNames(t, body, "local-model-form")), "a row for every reference the workflow takes")
			form := formOf(t, body, "local-model-form")
			form.Set("name", "m")
			rec := s.do(http.MethodPost, paths.LocalEndpoint("box", "models"), form)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.JSONEq(t, templateJSON(t, id), string(savedWorkflow(t, s, "m")), "a model made from a template is the template's")

			edit := s.do(http.MethodGet, paths.LocalEndpoint("box", "models", "m", "edit"), nil)
			require.Equal(t, http.StatusOK, edit.Code)
			assert.Equal(t, wantSlots, referenceSlotNames(fieldNames(t, edit.Body.String(), "local-model-form")))
			ef := formOf(t, edit.Body.String(), "local-model-form")
			ef.Set("cost_per_image_usd", "0.5")
			up := s.do(http.MethodPut, paths.LocalEndpoint("box", "models", "m"), ef)
			require.Equal(t, http.StatusOK, up.Code, up.Body.String())
			assert.JSONEq(t, templateJSON(t, id), string(savedWorkflow(t, s, "m")), "an unrelated edit changes nothing of the workflow")
			assert.Contains(t, s.savedFile(), `"cost_per_image_usd": 0.5`)
		})
	}
}

// What the form does edit is edited: a row put somewhere else, a row cleared, the output node. The rest stays.
func TestModel_EditingTheTableKeepsTheFlagsOfTheRowsThatStay(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188").Code)
	form := formOf(t, startWith(t, s, "box", "txt2img-qwen-image21"), "local-model-form")
	form.Set("name", "q")
	require.Equal(t, http.StatusOK, s.do(http.MethodPost, paths.LocalEndpoint("box", "models"), form).Code)

	edit := s.do(http.MethodGet, paths.LocalEndpoint("box", "models", "q", "edit"), nil)
	ef := formOf(t, edit.Body.String(), "local-model-form")
	ef.Set("assign_reference_image_9", "") // the tenth slot is given up
	ef.Set("assign_seed", "")              // the seed is not put anywhere
	require.Equal(t, http.StatusOK, s.do(http.MethodPut, paths.LocalEndpoint("box", "models", "q"), ef).Code)

	var w localcfg.Workflow
	require.NoError(t, json.Unmarshal(savedWorkflow(t, s, "q"), &w))
	assert.Equal(t, localcfg.ModeEdit, w.ReferenceMode)
	assert.Equal(t, "37", w.LoraModelNode)
	assert.Equal(t, 9, w.Count(localcfg.ParamReferenceImage))
	assert.False(t, w.Has(localcfg.ParamSeed))
	for _, a := range w.Assignments {
		if a.Param == localcfg.ParamReferenceImage {
			assert.True(t, a.Optional, "reference %d stays optional", a.Index)
		}
	}
}

// A refused save shows the form again with as many reference rows as it was sent with.
func TestModel_RefusedEditStillShowsAllReferenceRows(t *testing.T) {
	s := newScreen(t, false, "")
	require.Equal(t, http.StatusOK, s.addEndpoint("box", "comfyui", "http://127.0.0.1:8188").Code)
	form := formOf(t, startWith(t, s, "box", "txt2img-qwen-image21"), "local-model-form")
	form.Set("name", "q")
	form.Set("assign_prompt", "")
	rec := s.do(http.MethodPost, paths.LocalEndpoint("box", "models"), form)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, 10, referenceSlotNames(fieldNames(t, rec.Body.String(), "local-model-form")))
	assert.Equal(t, "txt2img-qwen-image21", formOf(t, rec.Body.String(), "local-model-form").Get("template"))
}
