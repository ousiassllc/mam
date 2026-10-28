package pages_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/credentials"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/local"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/mcp"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/media"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

var helpPanel = regexp.MustCompile(`(?s)<div id="(help-[0-9a-f]+)" popover[^>]*data-help-panel[^>]*>.*?</div>`)

// split gives the text of a page outside the help panels and the text inside them.
func split(t *testing.T, c templ.Component) (visible, inHelp string) {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	html := sb.String()
	panels := helpPanel.FindAllString(html, -1)
	return helpPanel.ReplaceAllString(html, ""), strings.Join(panels, "\n")
}

// The long explanations of a screen are in the "?" help (hidden until it is opened), not on the page (#866): each one is
// in a popover (closed until it is opened), is not anywhere else, and the panel is named by the button that opens it. What is not an explanation
// (the labels, the states) stays on the page.
func TestScreens_LongExplanationsAreInTheHelpNotOnThePage(t *testing.T) {
	tests := []struct {
		name    string
		page    templ.Component
		moved   []string
		visible []string
	}{
		{"model manager", pages.ModelManagerPage("tok", modelmgr.Data{}),
			[]string{"画像・動画のモデルを動かす ComfyUI を、mam が取得して起動・停止します", "ダウンロードは取得元から直接で、SHA-256 を確かめます", "コミットに固定し、LFS の SHA-256 とサイズを確かめます"},
			[]string{"モデルマネージャー", "ローカルの画像・動画モデルを導入して、すぐに試し生成できます", "ComfyUI エンジン", "試し生成", "外部の ComfyUI"}},
		{"api keys and login", pages.SettingsAPIKeysBody(credentials.Data{}, &chatgpt.Data{}),
			[]string{"保存したキーは再起動なしで次の呼び出しから使われます", "個人（Go / Plus / Pro）は OpenAI 公式の Sign in with ChatGPT"},
			[]string{"API キー", "ChatGPT でログイン"}},
		{"llm", pages.SettingsLLMBody(llm.Data{}),
			[]string{"Agent ごとに、最初に呼ぶ LLM", "保存した割り当ては、次の起動から使われます", "chatgpt-subscription"},
			[]string{"テキスト生成（LLM）", "反映にはアプリの再起動が必要です"}},
		{"media", pages.SettingsModelsBody(media.Data{}),
			[]string{"画像の用途ごと・Quality Policy ごとに", "models.json に書かれ", "「高速版」は、高速版を持つモデルでだけ保存できます", "テイク数は 1 以上です"},
			[]string{"画像・動画モデル", "画像モデル（用途ごと）", "動画モデル（Quality Policy ごと）"}},
		{"local", pages.SettingsLocalModelsBody(local.Data{}),
			[]string{"OpenAI 互換の画像 API を登録し", "リンクローカルは使えません"},
			[]string{"ローカル・代替モデル"}},
		{"mcp", pages.SettingsMCPBody(mcp.Data{}),
			[]string{"MCP（Streamable HTTP）で行えます", "tailnet の IP（100.x）にできます"},
			[]string{"MCP サーバー"}},
		{"generation test", pages.GenerationTestPage("tok", gentest.Compose{Prompts: []gentest.PromptText{{}}}),
			[]string{"プロジェクトに属さない画像・動画の生成です", "結果は Take・コスト台帳には入りません"},
			[]string{"生成テスト"}},
		{"project settings", pages.SettingsPage("tok", organisms.SettingsBodyData{}),
			[]string{"FAST は少ない Take と高速モデル", "LOCK の制約に違反した Take・成果物はスコアに関係なく不合格", "テンプレートを更新・削除しても既存の Project には反映されません"},
			[]string{"Quality Policy", "Brand / Safety Constraints", "Brand Templates"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			visible, inHelp := split(t, tc.page)

			for _, text := range tc.moved {
				assert.Contains(t, inHelp, text, "the explanation is in a help panel")
				assert.NotContains(t, visible, text, "and not on the page")
			}
			for _, text := range tc.visible {
				assert.Contains(t, visible, text, "the title stays on the page")
			}
			for _, m := range helpPanel.FindAllStringSubmatch(inHelp, -1) {
				assert.Equal(t, 1, strings.Count(visible, `aria-controls="`+m[1]+`"`), "the panel %s is controlled by one button", m[1])
			}
		})
	}
}
