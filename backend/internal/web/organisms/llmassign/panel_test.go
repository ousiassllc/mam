package llmassign_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/llmassign"
)

func render(t *testing.T, d llmassign.Data) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, llmassign.Panel(d).Render(context.Background(), &sb))
	return sb.String()
}

func data() llmassign.Data {
	row := llmassign.Row{
		Agent: "director", Label: "Director",
		Primary:        llmassign.Target{Provider: "google", Model: "gemini-x"},
		Fallback:       llmassign.Target{Provider: "openai", Model: "gpt-x"},
		DefaultPrimary: llmassign.Target{Provider: "google", Model: "gemini-x"}, DefaultFallback: llmassign.Target{Provider: "openai", Model: "gpt-x"},
	}
	return llmassign.Data{Rows: []llmassign.Row{row}, Providers: []string{"anthropic", "google", "openai"}, MaxModelLength: 128}
}

// The reset button sits inside the save form: htmx would send the form's values with a DELETE as URL query
// parameters, so it sends none, and a save and a reset never run together.
func TestPanel_ResetButtonSendsNoFormValuesAndSharesTheFormSync(t *testing.T) {
	html := render(t, data())

	button := regexp.MustCompile(`<button[^>]*\shx-delete=[^>]*>`).FindString(html)
	require.NotEmpty(t, button)
	assert.Contains(t, button, `hx-params="none"`)
	assert.Contains(t, button, `hx-sync="closest form:drop"`)
	assert.Contains(t, button, `hx-confirm=`)
	assert.Regexp(t, `<form[^>]*hx-sync="this:drop"`, html)
}

// Every field is named by a label that says the Agent's role (primary / fallback) and what it is.
func TestPanel_EveryFieldHasALabel(t *testing.T) {
	html := render(t, data())

	for _, id := range []string{"llm-director-primary-provider", "llm-director-primary-model", "llm-director-fallback-provider", "llm-director-fallback-model"} {
		assert.Contains(t, html, `id="`+id+`"`)
		assert.Contains(t, html, `for="`+id+`"`)
	}
	assert.Contains(t, html, `<legend class="font-medium text-sm">Director</legend>`)
	assert.Contains(t, html, `maxlength="128"`)
	assert.Regexp(t, `<option value="google" selected`, html)
}

func TestPanel_APinnedFileIsAListWithoutAForm(t *testing.T) {
	d := data()
	d.Pinned = true

	html := render(t, d)

	assert.NotContains(t, html, "<form")
	assert.NotContains(t, html, "<input")
	assert.Contains(t, html, "起動オプション（--llm-config / MAM_LLM_CONFIG）で固定されています")
	assert.Contains(t, html, "google / gemini-x")
}

func TestPanel_ShowsTheNoticeTheErrorAndTheProblem(t *testing.T) {
	d := data()
	d.Notice, d.Error, d.Problem = "保存しました。反映にはアプリの再起動が必要です", "Director のモデル名を入力してください", "unknown field"

	html := render(t, d)

	assert.Contains(t, html, `role="status"`)
	assert.Contains(t, html, "保存しました。反映にはアプリの再起動が必要です")
	assert.Contains(t, html, `role="alert"`)
	assert.Contains(t, html, "Director のモデル名を入力してください")
	assert.Contains(t, html, "unknown field")
}
