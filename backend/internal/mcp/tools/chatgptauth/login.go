package chatgptauth

import (
	"context"
	"errors"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// LoginOutput is a login as the agent sees it. It holds no token.
type LoginOutput struct {
	LoginID string `json:"login_id" jsonschema:"get_chatgpt_login_status に渡す"`
	Account string `json:"account" jsonschema:"official（公式）または image（画像生成・非公式）"`
	Method  string `json:"method" jsonschema:"browser または device"`
	State   string `json:"state" jsonschema:"pending / succeeded / failed / cancelled"`
	Message string `json:"message,omitempty"`
	// AuthURL is the authorization URL of a browser login.
	AuthURL       string `json:"auth_url,omitempty" jsonschema:"ブラウザでのログインの認可 URL。mam が動いている PC のブラウザで開く（コールバックがその PC の 127.0.0.1 に来る）"`
	BrowserOpened bool   `json:"browser_opened,omitempty" jsonschema:"mam が自分の PC の既定ブラウザを開いた"`
	// VerificationURL and UserCode are the device login's page and code.
	VerificationURL string `json:"verification_url,omitempty" jsonschema:"デバイスコードのログイン: 利用者がどの端末でも開く URL"`
	UserCode        string `json:"user_code,omitempty" jsonschema:"デバイスコードのログイン: 利用者が URL で入力するコード。自分で始めたログインのときだけ入力させる"`
	IntervalSeconds int    `json:"interval_seconds,omitempty" jsonschema:"get_chatgpt_login_status を呼ぶ間隔の目安（秒）"`
	ExpiresAt       string `json:"expires_at,omitempty" jsonschema:"このログインの期限（RFC 3339）"`
	Note            string `json:"note,omitempty"`
}

func loginOutput(account string, l chatgpt.Login) LoginOutput {
	out := LoginOutput{
		LoginID: l.ID, Account: account, Method: string(l.Method), State: string(l.State), Message: l.Message,
		AuthURL: l.AuthURL, BrowserOpened: l.BrowserOpened, VerificationURL: l.VerificationURL, UserCode: l.UserCode,
		IntervalSeconds: int(l.Interval / time.Second),
	}
	if !l.ExpiresAt.IsZero() {
		out.ExpiresAt = l.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (h handlers) startLogin(_ context.Context, _ Input) (LoginOutput, error) {
	svc, err := h.svc()
	if err != nil {
		return LoginOutput{}, err
	}
	l, err := svc.Official().StartBrowserLogin()
	if err != nil {
		return LoginOutput{}, err
	}
	out := loginOutput("official", l)
	out.Note = "ブラウザは mam が動いている PC で開きます。リモート（tailnet など）から操作している場合は、その PC でこの auth_url を開いてログインしてください。公式のログインにデバイスコードはありません。"
	return out, nil
}

func (h handlers) startDevice(ctx context.Context, _ Input) (LoginOutput, error) {
	svc, err := h.svc()
	if err != nil {
		return LoginOutput{}, err
	}
	l, err := svc.StartImageDeviceLogin(ctx)
	switch {
	case errors.Is(err, chatgpt.ErrImageDisabled):
		return LoginOutput{}, &service.ConflictError{Message: "画像生成（非公式）が OFF です。set_chatgpt_image_generation で、リスクを確認したうえで ON にしてください"}
	case err != nil:
		return LoginOutput{}, err
	}
	out := loginOutput("image", l)
	out.Note = "利用者に verification_url を開いて user_code を入力してもらい、get_chatgpt_login_status で完了を確認します。"
	return out, nil
}

// LoginStatusInput names the login.
type LoginStatusInput struct {
	LoginID string `json:"login_id" jsonschema:"start_chatgpt_login / start_chatgpt_image_device_login が返した login_id"`
}

func (h handlers) loginStatus(_ context.Context, in LoginStatusInput) (LoginOutput, error) {
	svc, err := h.svc()
	if err != nil {
		return LoginOutput{}, err
	}
	for _, a := range []struct {
		name string
		acc  *chatgpt.Account
	}{{"official", svc.Official()}, {"image", svc.Image()}} {
		if l, ok := a.acc.LoginStatus(in.LoginID); ok {
			return loginOutput(a.name, l), nil
		}
	}
	return LoginOutput{}, service.ErrNotFound
}

// LogoutInput names the sign-in to end.
type LogoutInput struct {
	Account string `json:"account" jsonschema:"official（公式のログイン）または image（画像生成・非公式のログイン）"`
}

// LogoutOutput is what the sign-out did.
type LogoutOutput struct {
	Account         string `json:"account"`
	WasSignedIn     bool   `json:"was_signed_in"`
	RevokeConfirmed bool   `json:"revoke_confirmed" jsonschema:"OpenAI がリフレッシュトークンの失効を確認した。false のときは ChatGPT の設定（Settings）で mam の接続を解除するよう利用者に伝える"`
	Message         string `json:"message"`
}

func (h handlers) logout(ctx context.Context, in LogoutInput) (LogoutOutput, error) {
	svc, err := h.svc()
	if err != nil {
		return LogoutOutput{}, err
	}
	var acc *chatgpt.Account
	switch in.Account {
	case "official":
		acc = svc.Official()
	case "image":
		acc = svc.Image()
	default:
		return LogoutOutput{}, &service.ValidationError{Field: "account", Message: "official か image を指定してください"}
	}
	res, err := acc.Logout(ctx)
	if err != nil {
		return LogoutOutput{}, err
	}
	out := LogoutOutput{Account: in.Account, WasSignedIn: res.WasSignedIn, RevokeConfirmed: res.RevokeConfirmed}
	switch {
	case !res.WasSignedIn:
		out.Message = "ログインしていませんでした"
	case res.RevokeConfirmed:
		out.Message = "ログアウトしました。トークンを失効し、保存したトークンを削除しました"
	default:
		out.Message = "ログアウトし、保存したトークンを削除しました。ただし OpenAI 側での失効を確認できませんでした。ChatGPT の設定（Settings）で mam の接続を解除してください"
	}
	return out, nil
}
