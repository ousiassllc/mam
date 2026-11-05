package chatgpt_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/chatgpt"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

// The three login systems are three cards of one list in the order of the spec; there is no tab list left.
func TestPanel_HasACardForEachLoginSystemInOrder(t *testing.T) {
	html := render(t, view.Panel(view.Data{}))

	assert.NotContains(t, html, `role="tablist"`)
	assert.Equal(t, 3, strings.Count(html, `data-testid="chatgpt-card"`))
	official, image, codex := strings.Index(html, "chatgpt-card-official"), strings.Index(html, "chatgpt-card-image"), strings.Index(html, "chatgpt-card-codex")
	assert.True(t, 0 < official && official < image && image < codex)
	assert.Contains(t, html, `<ul id="chatgpt-cards" role="list"`)
}

func TestPanel_AnOfficialLoginTheOrganizationRefusedPointsToTheCodexCardAndSignsNothingOut(t *testing.T) {
	d := view.Data{}
	assert.NotContains(t, render(t, view.Panel(d)), `data-testid="chatgpt-use-codex"`)

	d.Official.Login = &view.Login{Failed: true, UseCodex: true, Message: "refused"}
	html := render(t, view.Panel(d))
	assert.Contains(t, html, `data-testid="chatgpt-use-codex-button"`)
	assert.Contains(t, html, `href="#chatgpt-card-codex"`, "the card is on the same page")
	assert.NotContains(t, html, `data-testid="chatgpt-logout-official"`)

	d = view.Data{Official: view.Account{SignedIn: true, WorkspacePlan: true, Plan: "enterprise"}}
	assert.Contains(t, render(t, view.Panel(d)), `data-testid="chatgpt-use-codex"`)
}

// While the engine downloads, the panel reads itself again and shows the progress with a way to stop it.
func TestPanel_TheEngineDownloadPollsAndHasACancel(t *testing.T) {
	d := view.Data{}
	d.Codex.Engine = view.CodexEngine{State: "installing", Supported: true, Install: view.CodexInstall{Running: true, Percent: 37, Message: "ダウンロード中"}}
	html := render(t, view.Panel(d))
	assert.Contains(t, html, `hx-get="/settings/chatgpt"`)
	assert.Contains(t, html, `hx-trigger="every 2s"`)
	assert.Contains(t, html, "37%")
	assert.Contains(t, html, `data-testid="chatgpt-codex-install-cancel"`)
	assert.NotContains(t, html, `data-testid="chatgpt-codex-install-button"`)
	assert.Contains(t, html, "商用・ホスティング", "the terms are on the card")

	d.Codex.Engine = view.CodexEngine{State: "not_installed", Supported: true}
	html = render(t, view.Panel(d))
	assert.Contains(t, html, `data-testid="chatgpt-codex-install-button"`)
	assert.NotContains(t, html, `hx-trigger="every 2s"`)
	assert.NotContains(t, html, "約 0 MB", "sizes are shown only when the engine says them")
}

// The notice before the download names the size of the asset of this platform and where it is put, and the
// "unsupported" note is for the platforms that have no download (macOS is no longer one of them).
func TestPanel_TheDownloadNoticeNamesTheSizesAndThePlace(t *testing.T) {
	d := view.Data{}
	d.Codex.Engine = view.CodexEngine{State: "not_installed", Supported: true, DownloadBytes: 78_849_710, ExtractedBytes: 201_321_648, Dir: "/Users/x/Library/Application Support/mam/codex"}
	html := render(t, view.Panel(d))
	assert.Contains(t, html, "約 79 MB。展開後は約 201 MB。")
	assert.Contains(t, html, "置き場所は /Users/x/Library/Application Support/mam/codex です")
	assert.NotContains(t, html, `data-testid="chatgpt-codex-unsupported"`)

	d.Codex.Engine = view.CodexEngine{State: "not_installed", Supported: false}
	html = render(t, view.Panel(d))
	assert.Contains(t, html, `data-testid="chatgpt-codex-unsupported"`)
	assert.Contains(t, html, "Windows 版と macOS 版（Apple Silicon）だけ")
	assert.NotContains(t, html, `data-testid="chatgpt-codex-install-button"`)
}

func TestPanel_TheUsageLimitResetsAreShownInJapanStandardTime(t *testing.T) {
	d := view.Data{}
	d.Codex = view.Codex{SignedIn: true, Email: "a@example.invalid", Plan: "business",
		Engine: view.CodexEngine{State: "running", Supported: true},
		Limits: []view.CodexLimit{{Name: "5 時間", UsedPercent: 100, ResetsAt: time.Date(2026, 10, 25, 9, 30, 0, 0, time.UTC)}}}
	html := render(t, view.Panel(d))

	assert.Contains(t, html, "5 時間: 100% 使用（リセット 2026-10-25 18:30（日本時間））")
	assert.Contains(t, html, `data-testid="chatgpt-logout-codex"`)
	assert.Contains(t, html, `data-testid="chatgpt-models-show-codex"`)
}

// The state of a card is a badge with words: a pending login says so, and a signed-in account that must log in again
// says that, so colour is never the only difference.
func TestPanel_TheStateOfACardIsSaidInWords(t *testing.T) {
	html := render(t, view.Panel(view.Data{
		Official: view.Account{SignedIn: true, NeedsRelogin: true, Email: "a@example.invalid"},
	}))
	assert.Contains(t, html, "再ログインが必要")
	assert.Contains(t, html, "エンジン未取得")
	assert.Contains(t, html, "オフ", "the image generation is off until its risks are acknowledged")
	assert.NotContains(t, html, `data-testid="chatgpt-image-ack"`, "the risks and the confirmation are in the detail")

	pending := render(t, view.Panel(view.Data{Official: view.Account{Login: &view.Login{Pending: true}}}))
	assert.Contains(t, pending, "ログイン待ち")
	assert.Contains(t, pending, `hx-trigger="every 2s"`, "the cards read themselves while a login is pending")
}

// The login modal asks the way (the official one has the browser only), shows the code of a device login that is
// pending, and never a token.
func TestLoginModal_ChoosesTheMethodOrShowsTheProgress(t *testing.T) {
	official := render(t, view.LoginModal(view.LoginForm{Kind: "official"}))
	assert.Contains(t, official, `<input type="hidden" name="method" value="browser">`)
	assert.NotContains(t, official, `value="device"`, "no device login for the official sign-in")
	assert.Contains(t, official, "Continue with ChatGPT")

	image := render(t, view.LoginModal(view.LoginForm{Kind: "image"}))
	assert.Contains(t, image, `value="browser"`)
	assert.Contains(t, image, `value="device"`)
	assert.Contains(t, image, "ログインを始める")

	pending := render(t, view.LoginModal(view.LoginForm{Kind: "image", Login: &view.Login{
		Pending: true, Device: true, VerificationURL: "https://auth.example.invalid/device", UserCode: "ABCD-1234", ExpiresAt: time.Now().Add(time.Minute),
	}}))
	assert.Contains(t, pending, "ABCD-1234")
	assert.Contains(t, pending, `hx-trigger="every 2s"`)
	assert.Contains(t, pending, `hx-get="/settings/chatgpt/image/login?poll=1"`)
	assert.Contains(t, pending, "ログインを中止")
	assert.NotContains(t, pending, `name="method"`, "no second login while one is pending")

	failed := render(t, view.LoginModal(view.LoginForm{Kind: "image", Login: &view.Login{Failed: true, Message: "期限が切れました"}}))
	assert.Contains(t, failed, `id="mam-modal-error"`)
	assert.Contains(t, failed, "期限が切れました")
	assert.Contains(t, failed, `name="method"`, "and it can be started again")
}

// A card says its state and, whatever the state, the risk of its route in a badge: "非公式" on the image generation and
// "商用注意" on Codex. The long text is not on the card: it is in the detail, which the card has a button for.
func TestPanel_TheRisksAreBadgesOnTheCardsAndTheLongTextIsInTheDetail(t *testing.T) {
	d := view.Data{Risks: []string{"リスクその 1", "リスクその 2"}}
	html := render(t, view.Panel(d))

	card := func(kind string) string {
		start := strings.Index(html, `id="chatgpt-card-`+kind+`"`)
		require.Positive(t, start)
		end := strings.Index(html[start:], "</li>")
		return html[start : start+end]
	}
	assert.Contains(t, card("image"), "非公式")
	assert.Contains(t, card("codex"), "商用注意")
	assert.NotContains(t, card("official"), "非公式")
	for _, kind := range []string{"official", "image", "codex"} {
		assert.Contains(t, card(kind), `hx-get="/settings/chatgpt/`+kind+`/detail"`, kind)
	}
	assert.NotContains(t, html, "リスクその 1", "the risks are in the detail")
	assert.NotContains(t, html, `data-help-toggle`, "no help buttons: the detail is the way to the long text")

	image := render(t, view.DetailModal(view.DetailForm{Kind: "image", Data: d}))
	assert.Contains(t, image, "リスクその 1")
	assert.Contains(t, image, `data-testid="chatgpt-image-ack"`)
	assert.Contains(t, image, `hx-target="#mam-modal-body"`, "the switch answers in the modal")

	d.ImageEnabled = true
	on := render(t, view.DetailModal(view.DetailForm{Kind: "image", Data: d}))
	assert.Contains(t, on, "リスクその 2", "the risks stay readable while it is on")
	assert.NotContains(t, on, `data-testid="chatgpt-image-ack"`)
	assert.Contains(t, render(t, view.Panel(d)), `data-testid="chatgpt-image-off"`, "the switch to turn it off is on the card")

	codex := render(t, view.DetailModal(view.DetailForm{Kind: "codex", Data: d}))
	assert.Contains(t, codex, `data-testid="chatgpt-codex-terms"`)
	assert.Contains(t, codex, "商用・ホスティング")
	assert.Contains(t, render(t, view.DetailModal(view.DetailForm{Kind: "official", Data: d})), `data-testid="chatgpt-official-detail"`)

	refused := render(t, view.DetailModal(view.DetailForm{Kind: "image", Data: d, Error: "だめでした"}))
	assert.Contains(t, refused, `id="mam-modal-error"`)
}

// The cards are in two columns like those of the API keys, and a long name or e-mail breaks instead of coming out.
func TestPanel_TheCardsAreInTwoColumnsAndAnAccountThatIsLongBreaks(t *testing.T) {
	d := view.Data{Official: view.Account{SignedIn: true, PlanUsageGranted: true, Email: strings.Repeat("a", 80) + "@example.invalid", Plan: "plus"}}
	html := render(t, view.Panel(d))

	assert.Contains(t, html, `<ul id="chatgpt-cards" role="list" class="grid grid-cols-1 gap-4 xl:grid-cols-2"`)
	assert.Contains(t, html, `class="break-all" data-testid="chatgpt-email"`)
}
