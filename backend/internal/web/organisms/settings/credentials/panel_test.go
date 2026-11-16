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

// The key is typed into a form that htmx sends as a PUT. Should the browser send it instead (before htmx has processed it),
// it must be in the body of a request and never in the address: the form is a POST.
func TestModal_TheKeyFormIsNeverAGetThatPutsTheKeyInTheAddress(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, credentials.Modal(credentials.Form{Provider: credentials.ProviderView{Provider: "kling", Label: "Kling"}}, 2560).Render(context.Background(), &sb))
	html := sb.String()

	assert.Contains(t, html, `method="post"`)
	assert.Contains(t, html, `hx-put="/credentials/kling"`)
	assert.Contains(t, html, `type="password"`)
	assert.NotContains(t, html, `value="`+"FAKE", "the key field is never filled")
}

func renderProviderChoice(t *testing.T, models []credentials.ProviderModel) string {
	t.Helper()
	return renderPanel(t, credentials.Data{
		Provider:  &credentials.ProviderData{Choices: []credentials.ProviderChoice{{Value: "openrouter", Label: "OpenRouter", Checked: true}}, Models: models},
		Providers: []credentials.ProviderView{{Provider: "openrouter", Label: "OpenRouter", Configured: false}},
	})
}

// Several models that lack the same key are one cause: one banner above the list with one button that opens the modal of the key, and a quiet chip on each row.
func TestPanel_ModelsThatLackTheSameKeyAreOneBannerWithOneButtonAndQuietChips(t *testing.T) {
	html := renderProviderChoice(t, []credentials.ProviderModel{
		{Label: "Kling 3.0", Supported: true, KeyLabel: "OpenRouter", KeyProvider: "openrouter"},
		{Label: "Kling 4.0", Supported: true, KeyLabel: "OpenRouter", KeyProvider: "openrouter"},
		{Label: "Veo", Supported: false, Why: "このプロバイダーでは扱っていません"},
	})

	assert.Equal(t, 1, strings.Count(html, `data-testid="api-provider-key-banner"`))
	assert.Equal(t, 1, strings.Count(html, `hx-get="/settings/credentials/openrouter/edit"`)-strings.Count(html, `aria-label="設定: OpenRouter"`), "the banner adds one button besides the card's")
	assert.Contains(t, html, "Kling 3.0、Kling 4.0 は、キーを入れると使えるようになります")
	assert.Equal(t, 3, strings.Count(html, "OpenRouter の API キーが未設定"), "the title of the banner and the chip of each row")
	assert.Contains(t, html, "bg-accent-soft text-mute")
	assert.NotContains(t, html, "warning")
}

// One model that lacks the key says so in its own row: no banner for a single line.
func TestPanel_OneModelThatLacksTheKeyHasNoBanner(t *testing.T) {
	html := renderProviderChoice(t, []credentials.ProviderModel{
		{Label: "Kling 3.0", Supported: true, KeyLabel: "OpenRouter", KeyProvider: "openrouter"},
		{Label: "Kling 4.0", Supported: true, KeyLabel: "OpenRouter", KeyProvider: "openrouter", KeySet: true},
	})

	assert.NotContains(t, html, `data-testid="api-provider-key-banner"`)
	assert.Contains(t, html, "OpenRouter の API キーが未設定")
	assert.NotContains(t, html, "warning")
}

// An unreadable key store and an unreadable settings file are a Banner and a Note in the ink of the page; the card says it cannot tell in a quiet chip.
func TestPanel_TheProblemsAreInTheInkOfThePageWithNoYellow(t *testing.T) {
	html := renderPanel(t, credentials.Data{Unavailable: true,
		Provider:  &credentials.ProviderData{Problem: "設定ファイルを読めません"},
		Providers: []credentials.ProviderView{{Provider: "kling", Label: "Kling"}}})

	assert.Contains(t, html, `data-testid="credential-unavailable"`)
	assert.Contains(t, html, `data-testid="api-provider-problem"`)
	assert.Contains(t, html, "border-l-2")
	assert.NotContains(t, html, "warning")
}
