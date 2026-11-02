package llm_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
)

func form(blame, message string) llm.Form {
	def := llm.Target{Provider: "google", Model: "gemini-x"}
	fallback := llm.Target{Provider: "openai", Model: "gpt-x"}
	field := func(role string, t llm.Target) llm.ModelField {
		return llm.ModelField{Agent: "director", Role: role, Provider: t.Provider, Selected: t.Model, Default: def, Options: []llm.ModelOption{{ID: t.Model}, {ID: "other-model", Name: "Other"}}}
	}
	return llm.Form{
		Row:       llm.Row{Agent: "director", Label: "Director", Purpose: "構成を計画する", Primary: def, Fallback: fallback, DefaultPrimary: def, DefaultFallback: def},
		Providers: []string{"anthropic", "google", "openai"}, MaxModelLength: 128, Error: message, Blame: blame,
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

// Every field is named by a label that says its role (primary / fallback) and what it is, and the fieldset names the role.
func TestModal_EveryFieldHasALabelAndTheFirstOneStartsFocused(t *testing.T) {
	html := render(t, form("", ""))

	for _, id := range []string{"llm-primary-provider", "llm-primary-model", "llm-fallback-provider", "llm-fallback-model"} {
		assert.Contains(t, html, `id="`+id+`"`)
		assert.Contains(t, html, `for="`+id+`"`)
	}
	assert.Contains(t, html, `<legend class="mb-1 text-sm font-medium">プライマリ</legend>`)
	assert.Contains(t, html, `<legend class="mb-1 text-sm font-medium">代替</legend>`)
	assert.Equal(t, 1, strings.Count(html, "autofocus"))
	assert.Regexp(t, `<select[^>]*autofocus[^>]*name="primary.provider"|<select[^>]*name="primary.provider"[^>]*autofocus`, html)
	assert.Regexp(t, `<option value="openai" selected`, html)
	assert.NotContains(t, html, `aria-invalid`)
	assert.NotContains(t, html, `id="mam-modal-error"`)
}

// A refusal puts the alert first and marks, and focuses, the field it is about (and nothing else).
func TestModal_ARefusalNamesTheFieldToBlame(t *testing.T) {
	html := render(t, form("fallback.model", "Director の代替: モデル名を入力してください"))

	assert.Regexp(t, `<p id="mam-modal-error" role="alert"[^>]*>Director の代替`, html)
	assert.Equal(t, 1, strings.Count(html, `aria-invalid="true"`))
	assert.Regexp(t, `<select[^>]*aria-invalid="true"[^>]*name="fallback.model"|<select[^>]*name="fallback.model"[^>]*aria-invalid="true"`, html)
	assert.Equal(t, 1, strings.Count(html, "autofocus"))

	// A refusal that blames no field leaves the focus to the alert (the script moves it there).
	assert.NotContains(t, render(t, form("", "設定ファイルを読み込めません")), "autofocus")
}

// The model is a labelled pull-down; where its models come from is said next to it and tied to it (aria-describedby), and a
// saved model the list lacks is kept as a choice with a warning.
func TestModal_TheModelPullDownSaysWhereTheModelsComeFromAndWarnsOfAnUnlistedOne(t *testing.T) {
	f := form("", "")
	f.Primary.FromAPI = true
	f.Fallback.Reason, f.Fallback.Selected, f.Fallback.Unlisted = "API キーが登録されていません", "gpt-old", true
	html := render(t, f)

	assert.Contains(t, html, `<optgroup label="プロバイダーの API の一覧">`)
	assert.Contains(t, html, "出所: プロバイダーの API の一覧")
	assert.Contains(t, html, `<optgroup label="mam の既定候補">`)
	assert.Contains(t, html, "出所: mam の既定候補（API キーが登録されていません）")
	assert.Regexp(t, `<select[^>]*aria-describedby="llm-primary-model-note"`, html)
	assert.Contains(t, html, `id="llm-primary-model-note"`)
	assert.Contains(t, html, `<option value="other-model">Other（other-model）</option>`)
	assert.Contains(t, html, `<option value="gpt-old" selected>gpt-old</option>`)
	assert.Contains(t, html, "「gpt-old」は openai の一覧にありません。使えないかもしれません（保存はできます）")
	assert.Equal(t, 1, strings.Count(html, `data-testid="llm-fallback-model-unlisted"`))
	assert.NotContains(t, html, `data-testid="llm-primary-model-unlisted"`)
	assert.Regexp(t, `<label[^>]*for="llm-fallback-model-custom"`, html)
	assert.Regexp(t, `<button[^>]*id="llm-save"`, html)
}
