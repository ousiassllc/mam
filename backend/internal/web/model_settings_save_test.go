package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// A save of the whole assignment (PUT /settings/models, which the tests keep using) is shown again and is read by the startup
// loader; the answer is the category, to swap in place, and the reset is offered after it.
func TestModelSettings_SaveIsShownAgainAndRead_ByTheStartupLoader(t *testing.T) {
	file := filepath.Join(t.TempDir(), "data", "models.json")
	s := newModelScreen(t, file, false)

	form := modelDefaultForm()
	form.Set(media.ImageModelField("preview", media.SlotPrimary), "gpt-image-2")
	form.Set(media.PolicyTargetField("fast", media.SlotPrimary), "seedance-2.0@1080p")
	form.Set(media.PolicyTakesField("fast"), "5")
	rec := s.request(http.MethodPut, form)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "保存しました。すぐに反映されます")
	assert.Contains(t, rec.Body.String(), `id="model-assignment"`, "the answer is the category, to swap in place")
	assert.NotContains(t, rec.Body.String(), `id="global-settings-body"`)

	cfg, err := configcheck.Load(file)
	require.NoError(t, err)
	assert.Equal(t, modeladapter.ImageRoute{
		Primary:  modeladapter.ImageTarget{Model: domain.ModelGPTImage2},
		Fallback: modeladapter.ImageTarget{Model: domain.ModelNanoBananaLite},
	}, cfg.Images[modeladapter.PurposePreview])
	assert.Equal(t, modeladapter.PolicyRoute{
		Primary:  modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "1080p"},
		Fallback: modeladapter.DefaultConfig().Policies[domain.QualityFast].Fallback,
		Takes:    5,
	}, cfg.Policies[domain.QualityFast])
	assert.Equal(t, modeladapter.DefaultConfig().Images[modeladapter.PurposeNewReference], cfg.Images[modeladapter.PurposeNewReference])

	// The next visit shows what was saved, with the reset offered.
	assert.Contains(t, s.page(), `hx-delete="`+paths.ModelSettings()+`"`)
	modal := s.imageModal("preview")
	assert.Equal(t, "gpt-image-2", selectedOption(t, modal, media.ImageModelField("preview", media.SlotPrimary)))
	assert.Equal(t, "nano-banana-lite", selectedOption(t, modal, media.ImageModelField("preview", media.SlotFallback)))
	modal = s.policyModal("fast")
	assert.Equal(t, "seedance-2.0@1080p", selectedOption(t, modal, media.PolicyTargetField("fast", media.SlotPrimary)))
	assert.Equal(t, "5", numberValue(t, modal, media.PolicyTakesField("fast")))

	// Reset: the defaults are shown again and the file is gone.
	rec = s.request(http.MethodDelete, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "既定に戻しました。すぐに反映されます")
	assert.NoFileExists(t, file)
	assert.Equal(t, "flux-2", selectedOption(t, s.imageModal("preview"), media.ImageModelField("preview", media.SlotPrimary)))
	assert.Equal(t, "3", numberValue(t, s.policyModal("balanced"), media.PolicyTakesField("balanced")))
	assert.NotContains(t, s.page(), `hx-delete="`+paths.ModelSettings()+`"`)
}

// The modal of a row saves that row only: the answer is the row and what a save may change (the notes and the reset), out of
// band and with the row first, a toast, and the event that closes the modal; the other rows and the file's other keys stay.
func TestModelSettings_ARowIsSavedOnItsOwnAndTheAnswerReplacesItsRowAndTheReset(t *testing.T) {
	file := filepath.Join(t.TempDir(), "models.json")
	s := newModelScreen(t, file, false)
	require.NotContains(t, s.page(), `data-testid="model-reset"`)

	form := modelDefaultForm()
	form.Set(media.PolicyTargetField("fast", media.SlotPrimary), "seedance-2.0@1080p")
	form.Set(media.PolicyTakesField("fast"), "5")
	rec := s.requestTo(http.MethodPut, paths.ModelPolicy("fast"), form)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Equal(t, "modal-close", rec.Header().Get("HX-Trigger-After-Swap"))
	assert.True(t, strings.HasPrefix(body, `<tr id="media-policy-fast"`), "a <tr> is read as a row only at the head of the answer: %s", body)
	assert.Contains(t, body, `hx-swap-oob="true"`)
	assert.Contains(t, body, "seedance-2.0@1080p")
	assert.Contains(t, body, `id="model-status"`)
	assert.Contains(t, body, `data-testid="model-reset"`, "the reset is offered now that the file differs from the defaults")
	assert.Contains(t, body, "動画モデルを保存しました")
	assert.NotContains(t, body, `id="media-policy-balanced"`, "only the row that was saved")
	cfg, err := configcheck.Load(file)
	require.NoError(t, err)
	assert.Equal(t, 5, cfg.Policies[domain.QualityFast].Takes)
	assert.Equal(t, modeladapter.DefaultConfig().Policies[domain.QualityBalanced], cfg.Policies[domain.QualityBalanced])
	assert.Equal(t, modeladapter.DefaultConfig().Images, cfg.Images)

	// An unknown purpose or policy is 404 with a modal that says so, and nothing is written.
	for _, req := range []struct{ method, path string }{
		{http.MethodGet, paths.ModelImageEdit("nowhere")}, {http.MethodPut, paths.ModelImage("nowhere")},
		{http.MethodGet, paths.ModelPolicyEdit("nowhere")}, {http.MethodPut, paths.ModelPolicy("nowhere")},
	} {
		rec = s.requestTo(req.method, req.path, modelDefaultForm())
		assert.Equal(t, http.StatusNotFound, rec.Code, req.path)
		assert.Contains(t, rec.Body.String(), `id="mam-modal-error"`, req.path)
	}

	// Not through HTMX (the address typed in) the category page answers.
	req := httptest.NewRequest(http.MethodGet, paths.ModelImageEdit("preview"), nil)
	req.AddCookie(s.cookie)
	page := s.send(req)
	assert.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), `<h2 id="settings-category-title"`)
}

// A refused save of a row answers 422 with the modal again, says why in the alert and keeps what was typed (and, for the
// field that has to be read first, the focus), and writes nothing.
func TestModelSettings_ARefusedSaveSaysWhyKeepsTheModalAndWritesNothing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "models.json")
	s := newModelScreen(t, file, false)

	form := modelDefaultForm()
	form.Set(media.PolicyTargetField("fast", media.SlotPrimary), "kling-3.0@480p") // Kling cannot make 480p
	form.Set(media.PolicyTakesField("fast"), "7")
	rec := s.requestTo(http.MethodPut, paths.ModelPolicy("fast"), form)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Regexp(t, `<p id="mam-modal-error" role="alert"[^>]*>[^<]*解像度に対応していません`, rec.Body.String())
	assert.Equal(t, "7", numberValue(t, rec.Body.String(), media.PolicyTakesField("fast")), "what was typed is shown again")
	assert.Equal(t, "kling-3.0@480p", selectedOption(t, rec.Body.String(), media.PolicyTargetField("fast", media.SlotPrimary)), "the refused option is still the selected one")
	assert.NoFileExists(t, file)

	for name, mutate := range map[string]func(url.Values){
		"no takes":           func(f url.Values) { f.Set(media.PolicyTakesField("fast"), "0") },
		"takes not a number": func(f url.Values) { f.Set(media.PolicyTakesField("fast"), "many") },
		"a missing field":    func(f url.Values) { f.Del(media.PolicyTargetField("fast", media.SlotFallback)) },
	} {
		bad := modelDefaultForm()
		mutate(bad)
		rec := s.requestTo(http.MethodPut, paths.ModelPolicy("fast"), bad)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `id="mam-modal-error"`, name)
	}
	for name, mutate := range map[string]func(url.Values){
		"an unknown model": func(f url.Values) { f.Set(media.ImageModelField("preview", media.SlotPrimary), "dall-e") },
		"a video model for an image": func(f url.Values) {
			f.Set(media.ImageModelField("preview", media.SlotFallback), "kling-3.0")
		},
	} {
		bad := modelDefaultForm()
		mutate(bad)
		rec := s.requestTo(http.MethodPut, paths.ModelImage("preview"), bad)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `id="mam-modal-error"`, name)
	}

	// The whole save refuses the same and says so above the tables.
	form = modelDefaultForm()
	form.Set(media.PolicyTargetField("balanced", media.SlotPrimary), "kling-3.0@480p")
	rec = s.request(http.MethodPut, form)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="model-assignment-error"`)
	assert.Contains(t, rec.Body.String(), "解像度に対応していません")
	assert.NoFileExists(t, file)
}

func TestModelSettings_APinnedFileIsShownReadOnly(t *testing.T) {
	file := filepath.Join(t.TempDir(), "operator.json")
	const pinned = `{"policies":{"fast":{"primary":{"model":"kling-3.0","resolution":"1080p"},"fallback":{"model":"seedance-2.0","resolution":"480p"},"takes":9}}}`
	require.NoError(t, os.WriteFile(file, []byte(pinned), 0o600))
	s := newModelScreen(t, file, true)

	html := s.page()
	assert.Contains(t, html, "起動オプション（--model-config / MAM_MODEL_CONFIG）で固定されています")
	assert.Regexp(t, `(?s)id="media-policy-fast".*?kling-3.0@1080p.*?seedance-2.0@480p.*?<td[^>]*>\s*9\s*</td>`, html, "the operator's file is what is shown")
	assert.NotContains(t, html, `hx-delete="`+paths.ModelSettings()+`"`)
	assert.NotContains(t, html, "data-modal-open", "no button edits a row")
	assert.NotContains(t, html, `type="submit"`)

	for _, req := range []struct{ method, path string }{
		{http.MethodGet, paths.ModelImageEdit("preview")}, {http.MethodGet, paths.ModelPolicyEdit("fast")},
		{http.MethodPut, paths.ModelImage("preview")}, {http.MethodPut, paths.ModelPolicy("fast")},
		{http.MethodPut, paths.ModelSettings()}, {http.MethodDelete, paths.ModelSettings()},
	} {
		rec := s.requestTo(req.method, req.path, modelDefaultForm())
		assert.Equal(t, http.StatusConflict, rec.Code, req.method+" "+req.path)
		assert.NotContains(t, rec.Body.String(), `type="submit"`, "a refusal has nothing to save")
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
	assert.Contains(t, html, `<nav aria-label="設定"`, "the rest of the screen is still there")
	assert.Contains(t, html, "<h1")
	assert.Equal(t, http.StatusConflict, s.requestTo(http.MethodGet, paths.ModelImageEdit("preview"), nil).Code)
}
