package chat_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/resulttile"
)

// chatThread is the chat of the single generation (#1138) with a talk in it, as the handlers draw it: a question with cards, the models that were looked at,
// the confirmation card of an estimate with a long model name, the results of a batch and a refusal. The browser tests (static/src/components/gentest-chat/*.wtest.ts)
// read it. Write it again with `MAM_UPDATE_FIXTURES=1 go test ./backend/internal/web/organisms/gentest/chat`.
func chatThread() templ.Component {
	const session = "0123456789abcdef0123456789abcdef"
	msgs := []chat.Message{
		{ID: "u1", Kind: chat.KindUser, Text: "夕暮れの海辺にいる狐の画像を、いくつかのモデルで比べたいです。できるだけ安く済ませたいです。"},
		{ID: "a1", Kind: chat.KindAssistant, Text: "画像ですね。どれくらいの品質を求めますか？", Options: []chat.Option{
			{Label: "安く試したい", Detail: "1 枚 約 3 円のモデルから", Reply: "安く試したいです"},
			{Label: "きれいに仕上げたい", Detail: "1 枚 約 8 円〜。仕上がりを優先", Reply: "きれいに仕上げたいです"},
		}},
		{ID: "u2", Kind: chat.KindUser, Text: "安く試したいです"},
		{ID: "s1", Kind: chat.KindStatus, Text: "画像のモデルを調べました（6 件）"},
		{ID: "a2", Kind: chat.KindAssistant, Text: "FLUX.2 と Nano Banana の 2 つで見積りました。", Proposal: &chat.Proposal{
			ID: "p1", SessionID: session, State: "pending", Open: true, Items: 4, Total: "$0.1272",
			Facts:  []chat.KV{{Key: "種類", Value: "画像"}, {Key: "モデル", Value: "flux-2、nano-banana"}, {Key: "本数", Value: "2 本（モデルとプロンプトの組ごと）"}, {Key: "縦横比", Value: "1:1"}},
			Prompt: []chat.PromptText{{Text: "a fox standing on a beach at dusk, warm light, detailed fur", Negative: "blurry, low quality"}},
			Lines: []chat.Line{
				{Label: "FLUX.2 [max] (Black Forest Labs)", Items: 2, Unit: "$0.0300", Subtotal: "$0.0600"},
				{Label: "Nano Banana（ChatGPT サブスクリプションではない Google のモデル）", Items: 2, Unit: "$0.0336", Subtotal: "$0.0672"},
			},
			Warnings: []string{"nano-banana にはネガティブの欄がないため、「Avoid: …」としてプロンプトに書き足して送ります"},
		}},
		{ID: "r1", Kind: chat.KindResult, Result: &chat.Result{
			BatchID: "b1", State: "succeeded", StateTxt: "完了", Tone: atoms.BadgeOK, Done: 2, Total: 2, Finished: true, Counts: "成功 2・失敗 0・取り消し 0", Estimate: "$0.0636",
			Tiles: []chat.Tile{
				{Model: "FLUX.2 [max]", Card: resulttile.Card{ID: "r0", Model: "FLUX.2", Prompt: "a fox", Status: "succeeded", StatusText: "完了", Detail: "/generation-test/results/r0"}},
				{Model: "Nano Banana", Card: resulttile.Card{ID: "r1", Model: "Nano Banana", Prompt: "a fox", Status: "failed", StatusText: "失敗", Detail: "/generation-test/results/r1"}},
			},
		}},
		{ID: "e1", Kind: chat.KindStatus, Failed: true, Text: "実行しませんでした: 確認した見積りと今の見積りが違います。続けるには、もう一度見積りを取ってください", SettingsHref: "/settings/llm"},
	}
	return templ.Join(chat.Chat(chat.Panel{SessionID: session}), chat.Messages(session, msgs))
}

func TestFixtures_AreWhatTheComponentsRender(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, chatThread().Render(context.Background(), &buf))
	file := filepath.Join("..", "..", "..", "..", "..", "..", "static", "src", "components", "testdata", "gentest-chat-thread.html")
	if os.Getenv("MAM_UPDATE_FIXTURES") != "" {
		require.NoError(t, os.WriteFile(file, buf.Bytes(), 0o600)) // #nosec G306 -- a test fixture in the repository
		return
	}
	want, err := os.ReadFile(file) // #nosec G304 -- a fixed path inside the repository
	require.NoError(t, err, "write it with MAM_UPDATE_FIXTURES=1")
	assert.Equal(t, string(want), buf.String(), "the component changed: write the fixture again with MAM_UPDATE_FIXTURES=1")
}
