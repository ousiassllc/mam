package llm_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
)

func form(blame, message string) llm.Form {
	def := llm.Target{Provider: "google", Model: "gemini-x"}
	fallback := llm.Target{Provider: "openai", Model: "gpt-x"}
	field := func(role string, t llm.Target) llm.ModelField {
		f := modelpick.FieldData{
			ID: llm.FieldID(role), Label: "モデル", URL: "/model-picker/settings-llm",
			Inputs:  []modelpick.Input{{Name: role + ".provider", Key: "provider", Value: t.Provider}, {Name: role + ".model", Key: "model", Value: t.Model}},
			Current: modelcard.Card{Name: t.Model, Icon: atoms.IconText},
		}
		f.Invalid = blame == role+".provider" || blame == role+".model"
		f.Autofocus = f.Invalid || (blame == "" && message == "" && role == "primary")
		return llm.ModelField{Role: role, Provider: t.Provider, Selected: t.Model, Default: def, Field: f}
	}
	return llm.Form{
		Row:            llm.Row{Agent: "director", Label: "Director", Purpose: "構成を計画する", Primary: def, Fallback: fallback, DefaultPrimary: def, DefaultFallback: def},
		MaxModelLength: 128, Error: message, Blame: blame,
		Primary: field("primary", def), Fallback: field("fallback", fallback),
	}
}

func render(t *testing.T, f llm.Form) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, llm.Modal(f).Render(context.Background(), &sb))
	return sb.String()
}

// The reset button sits inside the save form: htmx would send the form's values with a DELETE as URL query parameters, so it
// sends none, and a save and a reset never run together. The form guards against a second submit like every posting form.
func TestModal_ResetButtonSendsNoFormValuesAndSharesTheFormSync(t *testing.T) {
	html := render(t, form("", ""))

	button := regexp.MustCompile(`<button[^>]*\shx-delete=[^>]*>`).FindString(html)
	require.NotEmpty(t, button)
	assert.Contains(t, button, `hx-params="none"`)
	assert.Contains(t, button, `hx-sync="closest form:drop"`)
	assert.Contains(t, button, `hx-disabled-elt="this"`, "the form's find button[type=submit] is looked up from the reset button and finds nothing")
	assert.Regexp(t, `<form[^>]*hx-sync="this:drop"[^>]*hx-disabled-elt="find button\[type=submit\]"`, html)
	assert.Regexp(t, `<form[^>]*method="post"`, html, "a form sent by the browser itself would still not put a value in the URL")
}

// The provider and the model of a target are one field, the model picker (the same filter bar, panel and cards as every list of models): its button shows the model
// and opens the picker, and the choice is posted as the two hidden inputs the form always had. The fieldset names the role, and the first field starts focused.
func TestModal_TheTargetsAreModelPickerFieldsThatPostTheProviderAndTheModel(t *testing.T) {
	html := render(t, form("", ""))

	assert.NotContains(t, html, "<select", "no pull-down: the picker is the way to choose")
	assert.Contains(t, html, `<legend class="mb-1 text-sm">プライマリ</legend>`)
	assert.Contains(t, html, `<legend class="mb-1 text-sm">代替</legend>`)
	assert.Contains(t, html, `<input type="hidden" name="primary.provider" value="google" data-mp-from="provider">`)
	assert.Contains(t, html, `<input type="hidden" name="primary.model" value="gemini-x" data-mp-from="model">`)
	assert.Contains(t, html, `<input type="hidden" name="fallback.provider" value="openai" data-mp-from="provider">`)
	assert.Equal(t, 2, strings.Count(html, `hx-get="/model-picker/settings-llm"`))
	assert.Equal(t, 1, strings.Count(html, "autofocus"))
	assert.Regexp(t, `<button[^>]*id="llm-primary-model-button"[^>]*autofocus|<button[^>]*autofocus[^>]*id="llm-primary-model-button"`, html)
	assert.Contains(t, html, `既定: google / gemini-x`)
	assert.NotContains(t, html, `aria-invalid`)
	assert.NotContains(t, html, `id="mam-modal-error"`)
}

// A refusal puts the alert first and marks, and focuses, the field it is about (and nothing else).
func TestModal_ARefusalNamesTheFieldToBlame(t *testing.T) {
	html := render(t, form("fallback.model", "Director の代替: モデル名を入力してください"))

	assert.Regexp(t, `<p id="mam-modal-error" role="alert"[^>]*>Director の代替`, html)
	assert.Equal(t, 1, strings.Count(html, `aria-invalid="true"`))
	assert.Regexp(t, `<button[^>]*id="llm-fallback-model-button"[^>]*aria-invalid="true"|<button[^>]*aria-invalid="true"[^>]*id="llm-fallback-model-button"`, html)
	assert.Equal(t, 1, strings.Count(html, "autofocus"))

	// A refusal that blames no field leaves the focus to the alert (the script moves it there).
	assert.NotContains(t, render(t, form("", "設定ファイルを読み込めません")), "autofocus")
}

// A saved model the provider does not list stays, with a warning, and the save is not refused; a provider that cannot be used says why and where it is fixed.
func TestModal_AnUnlistedModelAndAnUnusableProviderAreWarnedAndStillSave(t *testing.T) {
	f := form("", "")
	f.Fallback.Unlisted, f.Fallback.Selected = true, "gpt-old"
	f.Fallback.Unusable, f.Fallback.Route = "API キーが登録されていません", "/settings/api-keys"
	html := render(t, f)

	assert.Contains(t, html, "「gpt-old」は openai の一覧にありません。使えないかもしれません（保存はできます）")
	assert.Equal(t, 1, strings.Count(html, `data-testid="llm-fallback-model-unlisted"`))
	assert.NotContains(t, html, `data-testid="llm-primary-model-unlisted"`)
	assert.Regexp(t, `data-testid="llm-fallback-unusable"[^>]*>\s*openai は今は使えません: API キーが登録されていません（保存はできます）`, html)
	assert.Regexp(t, `<a[^>]*href="/settings/api-keys"[^>]*data-testid="llm-fallback-route"`, html, "the way to the setting")
	assert.NotContains(t, html, `data-testid="llm-primary-unusable"`)
	assert.Regexp(t, `<label[^>]*for="llm-fallback-model-custom"`, html)
}
