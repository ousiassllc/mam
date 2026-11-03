package llm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
)

func renderSection(t *testing.T, d llm.Data) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, llm.Section(d).Render(context.Background(), &sb))
	return sb.String()
}

// What the whole table's answers say: why the saved file cannot be read (an alert), a success (a status) and why a save
// was refused (an alert), each under its own test id.
func TestSection_ShowsTheNoticeTheErrorAndTheProblemEachWithItsRole(t *testing.T) {
	d := llm.Data{
		Providers: []string{"anthropic"},
		Notice:    "既定に戻しました。次の呼び出しからすぐ使われます（再起動は要りません）",
		Error:     "Director のモデル名を入力してください",
		Problem:   "unknown field",
	}

	html := renderSection(t, d)

	assert.Regexp(t, `<p[^>]*role="status"[^>]*data-testid="llm-notice">既定に戻しました。次の呼び出しからすぐ使われます（再起動は要りません）</p>`, html)
	assert.Regexp(t, `<p[^>]*role="alert"[^>]*data-testid="llm-error">Director のモデル名を入力してください</p>`, html)
	assert.Regexp(t, `<p[^>]*role="alert"[^>]*data-testid="llm-problem">[^<]*unknown field</p>`, html)
}

func TestSection_SaysNothingWhenThereIsNothingToSay(t *testing.T) {
	html := renderSection(t, llm.Data{Providers: []string{"anthropic"}})

	for _, id := range []string{"llm-notice", "llm-error", "llm-problem"} {
		assert.NotContains(t, html, `data-testid="`+id+`"`)
	}
	assert.NotContains(t, html, `role="status"`)
	assert.NotContains(t, html, `role="alert"`)
}

// The restart notice is what an operation depends on, so it stays on the page; the longer explanation (when a saved
// assignment is used, the ChatGPT plan) is only in the help beside it, and the pinned notice and the errors stay visible
// without opening it.
func TestSection_KeepsTheApplyNoticeVisibleAndPutsTheLongTextInTheHelp(t *testing.T) {
	html := renderSection(t, llm.Data{Providers: []string{"anthropic"}, Pinned: true, Error: "失敗"})

	panelAt := strings.Index(html, "data-help-panel")
	require.GreaterOrEqual(t, panelAt, 0)
	panelEnd := panelAt + strings.Index(html[panelAt:], "</div>")
	visible := html[:panelAt] + html[panelEnd:]
	assert.Contains(t, visible, `data-testid="llm-apply-note">保存した割り当ては、次の呼び出しからすぐ使われます（再起動は要りません）。</p>`)
	assert.Contains(t, visible, `data-testid="llm-pinned"`)
	assert.Contains(t, visible, `role="alert" data-testid="llm-error"`)
	assert.NotContains(t, visible, "llm-chatgpt-note")
	assert.NotContains(t, visible, "実行中の呼び出しは")
	assert.Contains(t, html[panelAt:panelEnd], `data-testid="llm-chatgpt-note"`)
	assert.Contains(t, html[panelAt:panelEnd], "実行中の呼び出しは")
}
