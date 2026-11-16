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
		Notice:  "既定に戻しました。次の呼び出しからすぐ使われます（再起動は要りません）",
		Error:   "Director のモデル名を入力してください",
		Problem: "unknown field",
	}

	html := renderSection(t, d)

	assert.Regexp(t, `<p[^>]*role="status"[^>]*data-testid="llm-notice">既定に戻しました。次の呼び出しからすぐ使われます（再起動は要りません）</p>`, html)
	assert.Regexp(t, `<p[^>]*role="alert"[^>]*data-testid="llm-error">Director のモデル名を入力してください</p>`, html)
	assert.Regexp(t, `(?s)role="alert" data-testid="llm-problem">.*保存済みの設定ファイルを読み込めません.*<p[^>]*data-testid="llm-problem-detail">unknown field</p>`, html)
}

func TestSection_SaysNothingWhenThereIsNothingToSay(t *testing.T) {
	html := renderSection(t, llm.Data{})

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
	html := renderSection(t, llm.Data{Pinned: true, Error: "失敗"})

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

// Several saved routes that name a provider that cannot be used are ONE banner with one line for each route and one button for each distinct screen that fixes
// them (not one link for each line), in the ink of the page: no yellow.
func TestSection_TheRoutesThatCannotBeUsedAreOneBannerWithOneButtonForEachScreen(t *testing.T) {
	d := llm.Data{Warnings: []llm.Warning{
		{Provider: "openai", Reason: "API キーが登録されていません", Uses: []string{"Director のプライマリ", "Writer の代替"}, Route: "/settings/api-keys"},
		{Provider: "google", Reason: "API キーが登録されていません", Uses: []string{"Director の代替"}, Route: "/settings/api-keys"},
		{Provider: "chatgpt-codex", Reason: "ログインしていません", Uses: []string{"Writer のプライマリ"}, Route: "/settings/credentials"},
		{Provider: "x", Reason: "使えません", Uses: []string{"Writer の代替"}},
	}}

	html := renderSection(t, d)

	assert.Equal(t, 1, strings.Count(html, `data-testid="llm-warnings"`), "one banner")
	assert.Equal(t, 4, strings.Count(html, `data-testid="llm-warning"`), "one line for each cause")
	assert.Contains(t, html, "Director のプライマリ、Writer の代替 が指しています", "the routes of a cause are told in its line")
	assert.Equal(t, 2, strings.Count(html, `data-testid="llm-warning-link"`), "one button for each distinct screen")
	assert.Equal(t, 1, strings.Count(html, `href="/settings/api-keys"`))
	assert.Equal(t, 1, strings.Count(html, `href="/settings/credentials"`))
	for _, yellow := range []string{"text-warning", "border-warning", "bg-warning"} {
		assert.NotContains(t, html, yellow)
	}
}

// The key banner is one banner with a line for each missing key and the one button to the API keys; a row says "キー未登録" under the target without a key (the
// primary column and the alternative one alike) and that it runs on its alternative, in the ink of the page: no yellow.
func TestSection_TheMissingKeysAreOneBannerAndTheRowsSayWhichModelLacksItsKey(t *testing.T) {
	chip := llm.Chip{Label: "キー未登録", Title: "API キーが登録されていません"}
	d := llm.Data{
		KeyBanner: &llm.KeyBanner{
			Rows: 12, Blocked: 5, Covered: 4, Route: "/settings/api-keys",
			Shortages: []llm.KeyShortage{{Provider: "Anthropic（Claude）", Agents: []string{"Director", "Storyboard", "Prompt Planner", "Repair", "Command Interpreter"}}, {Provider: "Google（Gemini）", Agents: []string{"Art Director", "Video Judge", "Editor", "State Extractor"}}},
		},
		Rows: []llm.Row{
			{Agent: "art_director", Label: "Art Director", Purpose: "参照画像を解析する", Primary: llm.Target{Provider: "google", Model: "g"}, Fallback: llm.Target{Provider: "openai", Model: "o"}, PrimaryChip: chip, Note: "代替で動きます: 主モデルが使えないため、代替モデル o を使います"},
			{Agent: "director", Label: "Director", Purpose: "構成を計画する", Primary: llm.Target{Provider: "anthropic", Model: "c"}, Fallback: llm.Target{Provider: "google", Model: "g"}, PrimaryChip: chip, FallbackChip: chip},
		},
	}

	html := renderSection(t, d)

	assert.Equal(t, 1, strings.Count(html, `data-testid="llm-key-banner"`))
	assert.Contains(t, html, "API キーが足りないため、Agent 12件のうち 5 件で使えません。4 件は代替モデルで動いています")
	assert.Equal(t, 2, strings.Count(html, `data-testid="llm-key-shortage"`), "one line for each key")
	assert.Contains(t, html, "のキーを入れると、Director・Storyboard など 5 件が使えるようになります")
	assert.Equal(t, 1, strings.Count(html, `href="/settings/api-keys"`))
	assert.Equal(t, 3, strings.Count(html, `data-testid="llm-chip"`), "the targets without a key: one in the first row, two in the second")
	assert.Equal(t, 2, strings.Count(html, "代替で動きます"), "under the purpose, and under the name of the Agent for a narrow window")
	for _, yellow := range []string{"text-warning", "border-warning", "bg-warning"} {
		assert.NotContains(t, html, yellow)
	}
}

func TestSection_TheUnreadableFileIsOneAlertBannerWithoutYellow(t *testing.T) {
	html := renderSection(t, llm.Data{Problem: "unknown field"})

	assert.Equal(t, 1, strings.Count(html, `data-testid="llm-problem"`))
	assert.Regexp(t, `role="alert" data-testid="llm-problem"`, html)
	for _, yellow := range []string{"text-warning", "border-warning", "bg-warning"} {
		assert.NotContains(t, html, yellow)
	}
}
