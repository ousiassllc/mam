package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestModelSettings_TheScreenListsEveryPurposeAndPolicyAsARowAndTheirModalsOfferOnlyWhatTheAdapterCanRun(t *testing.T) {
	s := newModelScreen(t, filepath.Join(t.TempDir(), "data", "models.json"), false)
	html := s.page()

	assert.Contains(t, html, `id="model-assignment"`)
	assert.Contains(t, html, `<h2 id="settings-category-title"`)
	assert.NotContains(t, html, `hx-delete="`+paths.ModelSettings()+`"`, "nothing is saved: nothing to reset")
	assert.Equal(t, len(modeladapter.ImagePurposes()), strings.Count(html, `data-testid="model-assignment-image"`))
	assert.Equal(t, len(domain.QualityPolicies()), strings.Count(html, `data-testid="model-assignment-policy"`))
	assert.NotContains(t, html, "<select", "the models are chosen in the modal of a row")

	def := modeladapter.DefaultConfig()
	for _, p := range modeladapter.ImagePurposes() {
		assert.Contains(t, html, `id="media-image-`+string(p)+`"`)
		assert.Contains(t, html, `hx-get="`+paths.ModelImageEdit(string(p))+`"`)
		modal := s.imageModal(string(p))
		name := media.ImageModelField(string(p), media.SlotPrimary)
		assert.Equal(t, string(def.Images[p].Primary.Model), selectedOption(t, modal, name), p)
		assert.ElementsMatch(t, []string{"nano-banana", "nano-banana-lite", "flux-2", "gpt-image-2"}, optionValues(t, modal, name), "the models that have an image backend")
		for _, slot := range []string{media.SlotPrimary, media.SlotFallback} {
			assert.NotContains(t, modal, `name="`+media.ImageFastField(string(p), slot)+`"`, "no model has a fast variant by default: the checkbox is not offered: %s %s", p, slot)
		}
	}

	for _, q := range domain.QualityPolicies() {
		assert.Contains(t, html, `id="media-policy-`+string(q)+`"`)
		assert.Contains(t, html, `hx-get="`+paths.ModelPolicyEdit(string(q))+`"`)
		modal := s.policyModal(string(q))
		route := def.Policies[q]
		primary := media.PolicyTargetField(string(q), media.SlotPrimary)
		assert.Equal(t, string(route.Primary.Model)+"@"+route.Primary.Resolution, selectedOption(t, modal, primary), q)
		assert.Equal(t, strconv.Itoa(route.Takes), numberValue(t, modal, media.PolicyTakesField(string(q))), q)

		// A model is offered only at the resolutions it can make: Kling has no 480p, Runway only 720p.
		options := optionValues(t, modal, primary)
		assert.Contains(t, options, "kling-3.0@1080p")
		assert.NotContains(t, options, "kling-3.0@480p")
		assert.Contains(t, options, "runway-gen-4.5@720p")
		assert.NotContains(t, options, "runway-gen-4.5@1080p")
		for _, o := range options {
			model, res, _ := strings.Cut(o, "@")
			caps, ok := modeladapter.Capabilities(domain.ModelName(model))
			require.True(t, ok, o)
			assert.Contains(t, caps.Resolutions, res, o)
		}
	}
}

// noteOf is the reference-image note of the row of an image purpose ("" when it has none).
func noteOf(t *testing.T, html, purpose string) string {
	t.Helper()
	row := regexp.MustCompile(`(?s)data-purpose="` + purpose + `".*?</tr>`).FindString(html)
	require.NotEmpty(t, row, purpose)
	return regexp.MustCompile(`(?s)data-testid="model-assignment-reference-note"[^>]*>(.*?)</p>`).FindStringSubmatch(row + `<p data-testid="model-assignment-reference-note"></p>`)[1]
}

func TestModelSettings_OnlyARowThatNamesAModelWithoutReferenceImagesHasTheNote(t *testing.T) {
	s := newModelScreen(t, filepath.Join(t.TempDir(), "models.json"), false)
	html := s.page()

	assert.NotContains(t, html, "model-assignment-reference-warning", "the note is on the rows that need it, not above the tables")
	for _, p := range modeladapter.ImagePurposes() {
		assert.Empty(t, noteOf(t, html, string(p)), "every built-in model takes reference images: %s", p)
	}

	// Only a route that names a model that cannot take them has the note: a local model without an image input.
	t.Cleanup(func() { modelreg.Set(nil, nil) })
	modelreg.Set([]string{noRefLocalModel}, nil)
	file := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(file, []byte(`{
	  "endpoints": {"oa": {"kind": "openai-compat", "base_url": "http://192.168.1.9:8000"}},
	  "local_models": {"oa/x": {"media": "image", "model": "x", "accepts_image_inputs": false}},
	  "images": {
	    "new_reference": {"primary": {"model": "`+noRefLocalModel+`"}, "fallback": {"model": "nano-banana"}},
	    "edit_reference": {"primary": {"model": "nano-banana"}, "fallback": {"model": "`+noRefLocalModel+`"}}
	  }
	}`), 0o600))
	html = newModelScreen(t, file, false).page()
	assert.Contains(t, noteOf(t, html, "new_reference"), "主モデル "+noRefLocalModel+" は参照画像を使えません。このモデルで生成するときは、参照画像なしで Prompt だけから作ります")
	assert.NotContains(t, noteOf(t, html, "new_reference"), "代替モデル")
	assert.Contains(t, noteOf(t, html, "edit_reference"), "代替モデル "+noRefLocalModel+" は参照画像を使えません")
	assert.NotContains(t, noteOf(t, html, "edit_reference"), "主モデル")
	assert.Empty(t, noteOf(t, html, "preview"))
}

// noRefLocalModel is a local image model whose adapter drops reference images.
const noRefLocalModel = "openai-compat:oa/x"

func TestModelSettings_AModelsFileThatStillNamesImagen4IsShownAsNanoBanana(t *testing.T) {
	file := filepath.Join(t.TempDir(), "models.json")
	const legacy = `{
	  "models": {"imagen-4": {"model": "imagen-4.0-generate-001", "fast_model": "imagen-4.0-fast-generate-001", "cost_per_image_usd": 0.04}},
	  "images": {"new_reference": {"primary": {"model": "imagen-4"}, "fallback": {"model": "imagen-4", "fast": true}}}
	}`
	require.NoError(t, os.WriteFile(file, []byte(legacy), 0o600))
	s := newModelScreen(t, file, false)

	assert.NotContains(t, s.page(), `data-testid="model-assignment-problem"`)
	modal := s.imageModal("new_reference")
	assert.Equal(t, "nano-banana", selectedOption(t, modal, media.ImageModelField("new_reference", media.SlotPrimary)))
	assert.Equal(t, "nano-banana-lite", selectedOption(t, modal, media.ImageModelField("new_reference", media.SlotFallback)))
	assert.NotContains(t, modal, `name="`+media.ImageFastField("new_reference", media.SlotFallback)+`"`, "the fast flag is dropped")
	assert.NotContains(t, optionValues(t, modal, media.ImageModelField("new_reference", media.SlotPrimary)), "imagen-4")
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.JSONEq(t, legacy, string(data), "the file is read, not rewritten")
}

func TestModelSettings_TheFastVariantIsSavedOnlyForAModelThatHasOne(t *testing.T) {
	file := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"models":{"flux-2":{"model":"flux-2-pro","fast_model":"flux-2-klein","cost_per_image_usd":0.03}}}`), 0o600))
	s := newModelScreen(t, file, false)
	save := func(form url.Values) *httptest.ResponseRecorder {
		return s.requestTo(http.MethodPut, paths.ModelImage("preview"), form)
	}

	assert.False(t, isChecked(t, s.imageModal("preview"), media.ImageFastField("preview", media.SlotPrimary)))
	form := modelDefaultForm()
	form.Set(media.ImageFastField("preview", media.SlotPrimary), "1") // preview's primary is FLUX.2, which has a fast model here
	rec := save(form)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cfg, err := configcheck.Load(file)
	require.NoError(t, err)
	assert.Equal(t, modeladapter.ImageTarget{Model: domain.ModelFlux2, Fast: true}, cfg.Images[modeladapter.PurposePreview].Primary)
	assert.True(t, isChecked(t, s.imageModal("preview"), media.ImageFastField("preview", media.SlotPrimary)))

	// Nano Banana Lite (preview's fallback) has no fast variant by default: refused, with the reason.
	form = modelDefaultForm()
	form.Set(media.ImageFastField("preview", media.SlotFallback), "1")
	rec = save(form)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "nano-banana-lite には高速版がありません")
}

// With the age confirmation off the screen does not offer an adult model, but it shows a saved assignment that still names one as
// what it is (an option that says it cannot be used, not another model that would be saved by accident), says so above the
// form, and a save that keeps it is refused with the reason.
func TestModelSettings_AnAdultModelOfTheSavedAssignmentIsShownAsUnusableWhileTheAgeConfirmationIsOff(t *testing.T) {
	t.Cleanup(func() { modelreg.Set(nil, nil) })
	modelreg.Set([]string{noRefLocalModel}, nil)
	file := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(file, []byte(`{
	  "endpoints": {"oa": {"kind": "openai-compat", "base_url": "http://192.168.1.9:8000"}},
	  "local_models": {"oa/x": {"media": "image", "model": "x"}},
	  "images": {"new_reference": {"primary": {"model": "`+noRefLocalModel+`"}, "fallback": {"model": "nano-banana"}}}
	}`), 0o600))
	closed := true
	s := newGatedModelScreen(t, file, false, func(id string) bool { return closed && id == noRefLocalModel })
	primary := media.ImageModelField("new_reference", media.SlotPrimary)

	html := s.page()
	assert.Regexp(t, `(?s)id="media-image-new_reference".*?`+noRefLocalModel+`.*?（現在は使えません）`, html, "the row says the saved model cannot be used")
	assert.Contains(t, html, `data-testid="model-assignment-gated"`)
	assert.Contains(t, html, "年齢確認をオンにするか")
	modal := s.imageModal("new_reference")
	assert.NotContains(t, optionValues(t, s.imageModal("preview"), media.ImageModelField("preview", media.SlotPrimary)), noRefLocalModel, "not a choice")
	assert.Equal(t, noRefLocalModel, selectedOption(t, modal, primary), "the saved model is what the select shows")
	assert.Contains(t, modal, noRefLocalModel+"（現在は使えません）")

	// Saved unchanged the row is refused with the reason; the whole save refuses it as well.
	form := modelDefaultForm()
	form.Set(primary, noRefLocalModel)
	rec := s.requestTo(http.MethodPut, paths.ModelImage("new_reference"), form)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "年齢確認がオフ")
	assert.Equal(t, noRefLocalModel, selectedOption(t, rec.Body.String(), primary), "the refusal keeps the select as it was")
	rec = s.request(http.MethodPut, form)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "年齢確認がオフ")

	// Another row is saved all the same, and choosing another model for this one clears the note in the same answer.
	form = modelDefaultForm()
	form.Set(primary, "flux-2")
	rec = s.requestTo(http.MethodPut, paths.ModelImage("new_reference"), form)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), `data-testid="model-assignment-gated"`)
	assert.NotContains(t, rec.Body.String(), "（現在は使えません）")

	closed = false
	assert.Contains(t, optionValues(t, s.imageModal("preview"), media.ImageModelField("preview", media.SlotPrimary)), noRefLocalModel, "a choice again")
	assert.NotContains(t, s.page(), `data-testid="model-assignment-gated"`)
}
