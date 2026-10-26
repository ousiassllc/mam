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
		Notice:    "既定に戻しました。反映にはアプリの再起動が必要です",
		Error:     "Director のモデル名を入力してください",
		Problem:   "unknown field",
	}

	html := renderSection(t, d)

	assert.Regexp(t, `<p[^>]*role="status"[^>]*data-testid="llm-notice">既定に戻しました。反映にはアプリの再起動が必要です</p>`, html)
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
