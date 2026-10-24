// Package chatgptauth holds the MCP tools of the "ChatGPT でログイン" section of the settings screen: the two sign-ins
// (the official ChatGPT plan usage login for text generation and the opt-in unofficial image generation login), their
// status, the logout and the switch of the image generation. No tool returns a token: results are facts about a sign-in.
package chatgptauth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package. The ChatGPT service comes from r.Settings; a tool of an app that does not
// have it answers "unavailable".
func Register(r *mcpkit.Registrar, _ *service.Services) {
	h := handlers{r: r}
	mcpkit.Read(r, "get_chatgpt_status",
		"ChatGPT ログインの状態（公式のログイン・画像生成（非公式）のログイン・画像生成の ON/OFF）。メール・プラン・期限・再ログインの要否のみ。トークンは返さない / State of both ChatGPT sign-ins and the image generation switch. Never returns a token.",
		h.status)
	mcpkit.Write(r, "start_chatgpt_login",
		"公式の「Sign in with ChatGPT」（ChatGPT のサブスクをテキスト生成に使う）を始める。mam が動いている PC の既定ブラウザを開き、認可 URL も返す。コールバックはその PC の 127.0.0.1 に来るため、tailnet などリモートから使っている場合はその PC のブラウザで URL を開く必要がある（公式のログインにデバイスコードはない）。完了は get_chatgpt_login_status で確認 / Start the official Sign in with ChatGPT. The browser opens on the machine mam runs on and the callback comes to its 127.0.0.1: a remote user must open the URL on that machine (the official login has no device code).",
		h.startLogin, mcpkit.OpenWorld())
	mcpkit.Write(r, "start_chatgpt_image_device_login",
		"画像生成（非公式）のログインをデバイスコードで始める。確認用 URL とユーザーコードを返すので、どの端末のブラウザでも URL を開いてコードを入力する。自分で始めたログインのときだけコードを入力させること。画像生成（非公式）が ON のときだけ使える / Start the device-code login of the unofficial image generation and return the verification URL and the user code (only while it is ON).",
		h.startDevice, mcpkit.OpenWorld())
	mcpkit.Read(r, "get_chatgpt_login_status",
		"start_* が返した login_id のログインの進み具合（pending / succeeded / failed / cancelled）を返す。pending の間は interval 秒ごとに呼ぶ / Poll a login started by start_chatgpt_login or start_chatgpt_image_device_login.",
		h.loginStatus)
	mcpkit.Destroy(r, "logout_chatgpt",
		"ChatGPT からログアウトする。リフレッシュトークンの失効を OpenAI に要求し、保存したトークンを削除する（失効を確認できなくてもローカルのトークンは消える。その場合は応答で知らせる）/ Sign out: revoke the refresh token and delete the stored tokens.",
		h.logout, mcpkit.Idempotent(), mcpkit.OpenWorld())
	mcpkit.Write(r, "set_chatgpt_image_generation",
		"ChatGPT のサブスクでの画像生成（非公式）を ON/OFF する。既定は OFF。ON にするには acknowledge_risks=true が必要。リスク: "+chatgpt.ImageRisksSummary()+" / Turn the UNOFFICIAL ChatGPT-plan image generation on or off. ON requires acknowledge_risks=true: it uses an unofficial endpoint that can stop working, the official preview lists image generation as unsupported, and it consumes the plan's usage limit.",
		h.setImage, mcpkit.Idempotent())
}

type handlers struct{ r *mcpkit.Registrar }

func (h handlers) svc() (*chatgpt.Service, error) {
	if s := h.r.Settings.ChatGPT; s != nil {
		return s, nil
	}
	return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "ChatGPT のログインはこのアプリでは使えません", Status: http.StatusServiceUnavailable}
}

// Input has no field.
type Input struct{}

// Account is the state of one sign-in. It holds no token.
type Account struct {
	SignedIn bool `json:"signed_in"`
	// NeedsRelogin is true when the refresh token was refused for good: the tokens stay but only a new login helps.
	NeedsRelogin     bool   `json:"needs_relogin"`
	ReloginReason    string `json:"relogin_reason,omitempty"`
	Email            string `json:"email,omitempty"`
	Plan             string `json:"plan,omitempty" jsonschema:"ChatGPT のプラン（plus / pro など。ID トークンが伝えるときだけ）"`
	ExpiresAt        string `json:"access_token_expires_at,omitempty" jsonschema:"アクセストークンの期限（RFC 3339）。期限の前に自動で更新される"`
	PlanUsageGranted *bool  `json:"plan_usage_granted,omitempty" jsonschema:"公式ログインだけ: ChatGPT のプランの利用が許可されているか"`
}

func accountOf(s chatgpt.Status, official bool) Account {
	a := Account{SignedIn: s.SignedIn, NeedsRelogin: s.NeedsRelogin, ReloginReason: s.ReloginReason, Email: s.Email, Plan: s.Plan}
	if !s.ExpiresAt.IsZero() {
		a.ExpiresAt = s.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if official && s.SignedIn {
		granted := s.PlanUsageGranted
		a.PlanUsageGranted = &granted
	}
	return a
}

// StatusOutput is both sign-ins and the image generation switch.
type StatusOutput struct {
	Official               Account  `json:"official" jsonschema:"公式のログイン（テキスト生成。LLM の割り当てのプロバイダー chatgpt-subscription）"`
	Image                  Account  `json:"image" jsonschema:"画像生成（非公式）のログイン"`
	ImageGenerationEnabled bool     `json:"image_generation_enabled"`
	ImageRisks             []string `json:"image_generation_risks" jsonschema:"画像生成（非公式）の ON にする前に伝えるリスク"`
	UsageURL               string   `json:"usage_url" jsonschema:"ChatGPT のプランの利用状況を確認・制限するページ"`
}

func (h handlers) status(_ context.Context, _ Input) (StatusOutput, error) {
	svc, err := h.svc()
	if err != nil {
		return StatusOutput{}, err
	}
	ov, err := svc.Overview()
	if err != nil {
		return StatusOutput{}, err
	}
	return StatusOutput{
		Official: accountOf(ov.Official, true), Image: accountOf(ov.Image, false), ImageGenerationEnabled: ov.ImageEnabled,
		ImageRisks: chatgpt.ImageRisks, UsageURL: chatgpt.UsageURL,
	}, nil
}

// ImageSwitchInput turns the unofficial image generation on or off.
type ImageSwitchInput struct {
	Enabled          bool `json:"enabled" jsonschema:"true で ON、false で OFF"`
	AcknowledgeRisks bool `json:"acknowledge_risks,omitempty" jsonschema:"ON にするときだけ必須。画像生成（非公式）のリスク（非公式の経路・公式のプレビューではサポート外・プランの利用上限を消費）を利用者が確認したら true"`
}

// ImageSwitchOutput is the switch after the change, with the risks.
type ImageSwitchOutput struct {
	ImageGenerationEnabled bool     `json:"image_generation_enabled"`
	ImageRisks             []string `json:"image_generation_risks"`
	Next                   string   `json:"next,omitempty"`
}

func (h handlers) setImage(_ context.Context, in ImageSwitchInput) (ImageSwitchOutput, error) {
	svc, err := h.svc()
	if err != nil {
		return ImageSwitchOutput{}, err
	}
	if err := svc.SetImageEnabled(in.Enabled, in.AcknowledgeRisks); err != nil {
		if errors.Is(err, chatgpt.ErrRisksNotAcknowledged) {
			return ImageSwitchOutput{}, &service.ValidationError{Field: "acknowledge_risks", Message: "画像生成（非公式）をオンにするには、リスクを利用者に伝えて確認を取り、acknowledge_risks を true にしてください: " + chatgpt.ImageRisksSummary()}
		}
		return ImageSwitchOutput{}, err
	}
	out := ImageSwitchOutput{ImageGenerationEnabled: in.Enabled, ImageRisks: chatgpt.ImageRisks}
	if in.Enabled {
		out.Next = "start_chatgpt_image_device_login でログインしてください（または設定画面）"
	}
	return out, nil
}
