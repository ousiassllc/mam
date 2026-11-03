package errorbox_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/errorbox"
)

func render(t *testing.T, p errorbox.Props) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, errorbox.Box(p).Render(context.Background(), &sb))
	return sb.String()
}

func TestBox_CauseAndRemedyComeBeforeTheFoldedDetail(t *testing.T) {
	e := apperr.New(apperr.CodeInputMissing).With("param", "first_frame").WithDetail("modeladapter: endpoint:bundled-comfyui returned HTTP 422: the workflow needs first_frame (12)")
	e.ID = "err_abc"
	html := render(t, errorbox.Props{View: e.View(apperr.LangJa), Lang: apperr.LangJa, ID: "r1"})

	assert.Contains(t, html, `role="alert"`)
	assert.Contains(t, html, `data-error-code="input_missing"`)
	cause, remedy, details := strings.Index(html, "error-cause"), strings.Index(html, "error-remedy"), strings.Index(html, "<details")
	require.True(t, cause > 0 && remedy > cause && details > remedy, "the cause, the remedy, then the folded detail")
	assert.Contains(t, html, "最初のフレーム")
	assert.NotContains(t, html[:details], "returned HTTP 422", "the English detail is not outside the fold")
	assert.NotContains(t, html, "<details open")
	assert.Contains(t, html[details:], "詳細を見る")
	assert.Contains(t, html[details:], "returned HTTP 422: the workflow needs first_frame (12)")
	assert.Contains(t, html, "err_abc", "the error ID is shown")
}

func TestBox_TheCopyButtonReadsTheDetailField(t *testing.T) {
	html := render(t, errorbox.Props{View: apperr.New(apperr.CodeVRAM).WithDetail("CUDA out of memory").View(apperr.LangJa), Lang: apperr.LangJa, ID: "r1"})

	assert.Contains(t, html, `data-copy-source="error-detail-r1"`)
	assert.Contains(t, html, `id="error-detail-r1"`)
	assert.Contains(t, html, `data-copy-status="error-copy-status-r1"`)
	assert.Contains(t, html, `id="error-copy-status-r1"`)
	assert.Contains(t, html, `role="status"`)
	assert.Contains(t, html, "詳細をコピー")
	assert.Contains(t, html, "CUDA out of memory")
}

func TestBox_LinksToTheScreensThatHelp(t *testing.T) {
	html := render(t, errorbox.Props{View: apperr.New(apperr.CodeLLMKeyMissing).View(apperr.LangJa), Lang: apperr.LangJa, ID: "x"})

	assert.Contains(t, html, `href="/settings/api-keys"`)
	assert.Contains(t, html, `href="/settings/llm"`)

	none := render(t, errorbox.Props{View: apperr.New(apperr.CodeVRAM).View(apperr.LangJa), Lang: apperr.LangJa, ID: "x"})
	assert.NotContains(t, none, `data-testid="error-links"`)
}

func TestBox_ReadsInEnglishWhenAsked(t *testing.T) {
	html := render(t, errorbox.Props{View: apperr.New(apperr.CodeVRAM).View(apperr.LangEn), Lang: apperr.LangEn, ID: "x"})

	assert.Contains(t, html, "The GPU ran out of memory")
	assert.Contains(t, html, "Show the detail")
	assert.Contains(t, html, `data-copy-done="Copied"`)
	assert.NotContains(t, html, "原因")
}

func TestBox_EscapesTheDetail(t *testing.T) {
	html := render(t, errorbox.Props{View: apperr.New(apperr.CodeUnknown).WithDetail(`</textarea><script>alert(1)</script>`).View(apperr.LangJa), Lang: apperr.LangJa, ID: "x"})

	assert.NotContains(t, html, "<script>")
}
