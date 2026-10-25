package credentials_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/credentials"
)

func renderPanel(t *testing.T, d credentials.Data) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, credentials.Panel(d).Render(context.Background(), &sb))
	return sb.String()
}

func TestPanel_RendersACardForEachProviderWithItsPurposeAndState(t *testing.T) {
	html := renderPanel(t, credentials.Data{Providers: []credentials.ProviderView{
		{Provider: "kling", Label: "Kling", Purpose: "動画生成", Configured: true},
		{Provider: "runway", Label: "Runway", Purpose: "動画生成（Runway）", Configured: false},
	}})

	assert.Contains(t, html, `data-provider="kling"`)
	assert.Contains(t, html, `data-provider="runway"`)
	assert.Contains(t, html, "設定済み")
	assert.Contains(t, html, "未設定")
	assert.Contains(t, html, "動画生成（Runway）")
	assert.Contains(t, html, `hx-delete="/credentials/kling"`, "a set key can be deleted")
	assert.NotContains(t, html, `hx-delete="/credentials/runway"`)
	assert.Contains(t, html, `aria-label="変更: Kling"`)
	assert.Contains(t, html, `aria-label="設定: Runway"`)
	assert.Contains(t, html, `hx-confirm=`)
}

// The list holds no field: the key is typed into the modal, so there is nothing for htmx to send with a delete (a
// pasted-but-unsaved key in the URL, #504) and no password field left on the page.
func TestPanel_HasNoFormAndNoPasswordField(t *testing.T) {
	html := renderPanel(t, credentials.Data{Providers: []credentials.ProviderView{{Provider: "kling", Label: "Kling", Configured: true}}})

	assert.NotContains(t, html, "<form")
	assert.NotContains(t, html, "<input")
	assert.NotContains(t, html, `type="password"`)
}

// A key store that cannot be read says so, and no card can start a key: the buttons are disabled with the reason.
func TestPanel_UnavailableDisablesTheButtonsAndSaysWhy(t *testing.T) {
	html := renderPanel(t, credentials.Data{Unavailable: true, Providers: []credentials.ProviderView{{Provider: "kling", Label: "Kling", Configured: true}}})

	assert.Contains(t, html, `data-testid="credential-unavailable"`)
	assert.Contains(t, html, "状態を確認できません")
	assert.NotContains(t, html, "hx-get=")
	assert.NotContains(t, html, "hx-delete=")
	assert.Contains(t, html, `disabled`)
	assert.Contains(t, html, `title="キーストアを利用できません"`)
}

// A card answers for its provider only, so the cards of the others (and what is open in them) are not touched.
func TestRow_AnswersForItsProviderOnlyAndReplacesItselfOnlyOutOfBand(t *testing.T) {
	p := credentials.ProviderView{Provider: "kling", Label: "Kling", Configured: true}
	var plain, oob strings.Builder
	require.NoError(t, credentials.Row(p).Render(context.Background(), &plain))
	require.NoError(t, credentials.RowOOB(p).Render(context.Background(), &oob))

	assert.Contains(t, plain.String(), `id="credential-row-kling"`)
	assert.Contains(t, plain.String(), `hx-target="#credential-row-kling"`)
	assert.NotContains(t, plain.String(), "hx-swap-oob")
	assert.NotContains(t, plain.String(), `id="credential-panel"`)
	assert.Contains(t, oob.String(), `hx-swap-oob="true"`)
}
