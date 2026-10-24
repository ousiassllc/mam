package web_test

import (
	"net/http"
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
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelassignment"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestModelSettings_TheScreenListsEveryPurposeAndPolicyWithOnlyWhatTheAdapterCanRun(t *testing.T) {
	s := newModelScreen(t, filepath.Join(t.TempDir(), "data", "models.json"), false)
	html := s.page()

	assert.Contains(t, html, `id="model-assignment"`)
	assert.Contains(t, html, `hx-put="`+paths.ModelSettings()+`"`)
	assert.NotContains(t, html, `hx-delete="`+paths.ModelSettings()+`"`, "nothing is saved: nothing to reset")
	assert.Equal(t, len(modeladapter.ImagePurposes()), strings.Count(html, `data-testid="model-assignment-image"`))
	assert.Equal(t, len(domain.QualityPolicies()), strings.Count(html, `data-testid="model-assignment-policy"`))

	def := modeladapter.DefaultConfig()
	for _, p := range modeladapter.ImagePurposes() {
		name := modelassignment.ImageModelField(string(p), modelassignment.SlotPrimary)
		assert.Equal(t, string(def.Images[p].Primary.Model), selectedOption(t, html, name), p)
		assert.ElementsMatch(t, []string{"imagen-4", "nano-banana", "flux-2", "gpt-image-2"}, optionValues(t, html, name), "the models that have an image backend")
	}
	assert.True(t, isChecked(t, html, modelassignment.ImageFastField("preview", modelassignment.SlotFallback)), "the default preview fallback is the fast Imagen 4")
	assert.False(t, isChecked(t, html, modelassignment.ImageFastField("preview", modelassignment.SlotPrimary)))

	for _, q := range domain.QualityPolicies() {
		route := def.Policies[q]
		primary := modelassignment.PolicyTargetField(string(q), modelassignment.SlotPrimary)
		assert.Equal(t, string(route.Primary.Model)+"@"+route.Primary.Resolution, selectedOption(t, html, primary), q)
		assert.Equal(t, strconv.Itoa(route.Takes), numberValue(t, html, modelassignment.PolicyTakesField(string(q))), q)

		// A model is offered only at the resolutions it can make: Kling has no 480p, Runway only 720p.
		options := optionValues(t, html, primary)
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

func TestModelSettings_TheReferenceImageWarningStatesWhatTheAdapterDoes(t *testing.T) {
	s := newModelScreen(t, filepath.Join(t.TempDir(), "models.json"), false)
	html := s.page()

	assert.Contains(t, html, `data-testid="model-assignment-reference-warning"`)
	assert.Contains(t, html, "参照画像は渡されず、Prompt だけで画像を作ります")
	// Only a route that has such a model names it: the default new-reference route is Imagen 4, then GPT Image 2.
	noteOf := func(purpose string) string {
		row := regexp.MustCompile(`(?s)data-purpose="` + purpose + `".*?</li>`).FindString(html)
		require.NotEmpty(t, row, purpose)
		return regexp.MustCompile(`(?s)data-testid="model-assignment-reference-note"[^>]*>(.*?)</p>`).FindStringSubmatch(row + `<p data-testid="model-assignment-reference-note"></p>`)[1]
	}
	assert.Contains(t, noteOf("new_reference"), "主モデル imagen-4 は参照画像を使えません")
	assert.NotContains(t, noteOf("new_reference"), "代替モデル")
	assert.Contains(t, noteOf("edit_reference"), "代替モデル imagen-4 は参照画像を使えません")
	assert.NotContains(t, noteOf("edit_reference"), "主モデル")
}

func TestModelSettings_SaveIsShownAgainAndRead_ByTheStartupLoader(t *testing.T) {
	file := filepath.Join(t.TempDir(), "data", "models.json")
	s := newModelScreen(t, file, false)

	form := modelDefaultForm()
	form.Set(modelassignment.ImageModelField("preview", modelassignment.SlotPrimary), "imagen-4")
	form.Set(modelassignment.ImageFastField("preview", modelassignment.SlotPrimary), "1")
	form.Del(modelassignment.ImageFastField("preview", modelassignment.SlotFallback))
	form.Set(modelassignment.PolicyTargetField("fast", modelassignment.SlotPrimary), "seedance-2.0@1080p")
	form.Set(modelassignment.PolicyTakesField("fast"), "5")
	rec := s.request(http.MethodPut, form)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "保存しました。すぐに反映されます")
	assert.Contains(t, rec.Body.String(), `id="model-assignment"`, "the answer is the section, to swap in place")
	assert.NotContains(t, rec.Body.String(), `id="global-settings-body"`)

	cfg, err := configcheck.Load(file)
	require.NoError(t, err)
	assert.Equal(t, modeladapter.ImageRoute{
		Primary:  modeladapter.ImageTarget{Model: domain.ModelImagen4, Fast: true},
		Fallback: modeladapter.ImageTarget{Model: domain.ModelImagen4},
	}, cfg.Images[modeladapter.PurposePreview])
	assert.Equal(t, modeladapter.PolicyRoute{
		Primary:  modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "1080p"},
		Fallback: modeladapter.DefaultConfig().Policies[domain.QualityFast].Fallback,
		Takes:    5,
	}, cfg.Policies[domain.QualityFast])
	assert.Equal(t, modeladapter.DefaultConfig().Images[modeladapter.PurposeNewReference], cfg.Images[modeladapter.PurposeNewReference])

	// The next visit shows what was saved, with the reset offered.
	html := s.page()
	assert.Equal(t, "imagen-4", selectedOption(t, html, modelassignment.ImageModelField("preview", modelassignment.SlotPrimary)))
	assert.True(t, isChecked(t, html, modelassignment.ImageFastField("preview", modelassignment.SlotPrimary)))
	assert.False(t, isChecked(t, html, modelassignment.ImageFastField("preview", modelassignment.SlotFallback)))
	assert.Equal(t, "seedance-2.0@1080p", selectedOption(t, html, modelassignment.PolicyTargetField("fast", modelassignment.SlotPrimary)))
	assert.Equal(t, "5", numberValue(t, html, modelassignment.PolicyTakesField("fast")))
	assert.Contains(t, html, `hx-delete="`+paths.ModelSettings()+`"`)

	// Reset: the defaults are shown again and the file is gone.
	rec = s.request(http.MethodDelete, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "既定に戻しました。すぐに反映されます")
	assert.NoFileExists(t, file)
	html = s.page()
	assert.Equal(t, "flux-2", selectedOption(t, html, modelassignment.ImageModelField("preview", modelassignment.SlotPrimary)))
	assert.Equal(t, "3", numberValue(t, html, modelassignment.PolicyTakesField("balanced")))
	assert.NotContains(t, html, `hx-delete="`+paths.ModelSettings()+`"`)
}

func TestModelSettings_ARefusedSaveSaysWhyKeepsTheFormAndWritesNothing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "models.json")
	s := newModelScreen(t, file, false)

	form := modelDefaultForm()
	form.Set(modelassignment.PolicyTargetField("balanced", modelassignment.SlotPrimary), "kling-3.0@480p") // Kling cannot make 480p
	form.Set(modelassignment.PolicyTakesField("fast"), "7")
	rec := s.request(http.MethodPut, form)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="model-assignment-error"`)
	assert.Contains(t, rec.Body.String(), "解像度に対応していません")
	assert.Equal(t, "7", numberValue(t, rec.Body.String(), modelassignment.PolicyTakesField("fast")), "what was typed is shown again")
	assert.NoFileExists(t, file)

	for name, mutate := range map[string]func(url.Values){
		"no takes":           func(f url.Values) { f.Set(modelassignment.PolicyTakesField("fast"), "0") },
		"takes not a number": func(f url.Values) { f.Set(modelassignment.PolicyTakesField("fast"), "many") },
		"an unknown model": func(f url.Values) {
			f.Set(modelassignment.ImageModelField("preview", modelassignment.SlotPrimary), "dall-e")
		},
		"a video model for an image": func(f url.Values) {
			f.Set(modelassignment.ImageModelField("preview", modelassignment.SlotFallback), "kling-3.0")
		},
		"a missing field": func(f url.Values) { f.Del(modelassignment.PolicyTargetField("fast", modelassignment.SlotFallback)) },
	} {
		bad := modelDefaultForm()
		mutate(bad)
		rec := s.request(http.MethodPut, bad)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `data-testid="model-assignment-error"`, name)
	}
	assert.NoFileExists(t, file)
}

func TestModelSettings_APinnedFileIsShownReadOnly(t *testing.T) {
	file := filepath.Join(t.TempDir(), "operator.json")
	const pinned = `{"policies":{"fast":{"primary":{"model":"kling-3.0","resolution":"1080p"},"fallback":{"model":"seedance-2.0","resolution":"480p"},"takes":9}}}`
	require.NoError(t, os.WriteFile(file, []byte(pinned), 0o600))
	s := newModelScreen(t, file, true)

	html := s.page()
	assert.Contains(t, html, "起動オプション（--model-config / MAM_MODEL_CONFIG）で固定されています")
	assert.Equal(t, "9", numberValue(t, html, modelassignment.PolicyTakesField("fast")), "the operator's file is what is shown")
	assert.NotContains(t, html, `hx-delete="`+paths.ModelSettings()+`"`)
	form := regexp.MustCompile(`(?s)<form[^>]*id="model-assignment".*?</form>`).FindString(html)
	require.NotEmpty(t, form)
	assert.NotContains(t, form, `type="submit"`, "no save button")
	assert.Contains(t, form, "disabled", "the fields cannot be edited")

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := s.request(method, modelDefaultForm())
		assert.Equal(t, http.StatusConflict, rec.Code, method)
	}
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.JSONEq(t, pinned, string(data), "the operator's file is never written")
}

func TestModelSettings_AFileThatCannotBeReadOnlyLosesThisSection(t *testing.T) {
	file := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"unknown_key":1}`), 0o600))
	s := newModelScreen(t, file, false)

	html := s.page()
	assert.Contains(t, html, `data-testid="model-assignment-problem"`)
	assert.Contains(t, html, `id="credential-panel"`, "the rest of the screen is still there")
}
