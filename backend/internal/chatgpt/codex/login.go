package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// LoginMethod is how a login is done.
type LoginMethod string

// Login methods.
const (
	MethodBrowser LoginMethod = "browser"
	MethodDevice  LoginMethod = "device"
)

// LoginState is where a login is.
type LoginState string

// Login states.
const (
	LoginPending   LoginState = "pending"
	LoginSucceeded LoginState = "succeeded"
	LoginFailed    LoginState = "failed"
	LoginCancelled LoginState = "cancelled"
)

// Login timeouts: how long mam waits for the user (Codex has its own limits too).
const (
	browserLoginTimeout = 10 * time.Minute
	deviceLoginTimeout  = 15 * time.Minute
)

// Login is one login attempt as the screen and the MCP tools show it. It holds no token.
type Login struct {
	ID      string
	Method  LoginMethod
	State   LoginState
	Message string
	// AuthURL is the URL of a browser login: opened on this machine, shown as a fallback.
	AuthURL       string
	BrowserOpened bool
	// VerificationURL and UserCode are what the user enters on any device (device-code login).
	VerificationURL string
	UserCode        string
	ExpiresAt       time.Time
	FinishedAt      time.Time
}

type activeLogin struct {
	info  Login
	timer *time.Timer
}

// LoginStartError is a login that could not be started, with what the user can do about it.
type LoginStartError struct {
	Method  LoginMethod
	Message string
	Err     error
}

func (e *LoginStartError) Error() string { return "codex login: " + e.Message }

// Unwrap returns the cause.
func (e *LoginStartError) Unwrap() error { return e.Err }

// loginTimeoutMessage is the failure message of a login nobody finished.
const loginTimeoutMessage = "時間内にログインが完了しませんでした。もう一度始めてください"

// StartLogin starts a browser or device-code login: the app-server is started if needed and gives the URL (opened in the
// default browser of this machine) or the page and code. The login runs in the app-server; poll LoginStatus. A login in
// progress is cancelled first (the callback port and the user's attention are single).
func (s *Service) StartLogin(ctx context.Context, method LoginMethod) (Login, error) {
	var typ string
	switch method {
	case MethodBrowser:
		typ = protocol.LoginBrowser
	case MethodDevice:
		typ = protocol.LoginDeviceCode
	default:
		return Login{}, fmt.Errorf("codex: unknown login method %q", method)
	}
	se, err := s.session(ctx)
	if err != nil {
		return Login{}, err
	}
	s.cancelCurrent(se, "新しいログインを始めたため中止しました")
	var res protocol.LoginResult
	if err := se.Call(ctx, protocol.MethodLoginStart, protocol.LoginParams{Type: typ}, &res); err != nil {
		return Login{}, &LoginStartError{Method: method, Message: loginStartMessage(method, err), Err: err}
	}
	timeout := browserLoginTimeout
	if method == MethodDevice {
		timeout = deviceLoginTimeout
	}
	if s.opts.LoginTimeout > 0 {
		timeout = s.opts.LoginTimeout
	}
	l := Login{ID: res.LoginID, Method: method, State: LoginPending, AuthURL: res.AuthURL, VerificationURL: res.VerificationURL,
		UserCode: res.UserCode, ExpiresAt: s.now().Add(timeout)}
	switch method {
	case MethodBrowser:
		l.Message = "ブラウザで ChatGPT にログインしてください"
		if s.opts.Open != nil && res.AuthURL != "" {
			if err := s.opts.Open(res.AuthURL); err != nil {
				s.opts.Logger.Warn("codex login: could not open the browser")
				l.Message = "ブラウザを開けませんでした。表示した URL をこの PC のブラウザで開いてください"
			} else {
				l.BrowserOpened = true
			}
		}
	default:
		l.Message = "別の端末でも構いません。表示した URL を開き、コードを入力してください"
	}
	s.loginMu.Lock()
	a := &activeLogin{info: l}
	a.timer = time.AfterFunc(timeout, func() { s.expire(l.ID) })
	s.login = a
	s.loginMu.Unlock()
	return l, nil
}

// loginStartMessage is what to tell the user of a failed start. The text of the app-server is included, shortened.
func loginStartMessage(method LoginMethod, err error) string {
	detail := sanitize(err.Error())
	if method == MethodDevice {
		return "デバイスコードのログインを始められませんでした。ワークスペースの権限でデバイスコードのログインが有効になっている必要があります（beta）。Codex のメッセージ: " + detail
	}
	return "ログインを始められませんでした。Codex のメッセージ: " + detail
}

// CurrentLogin is the latest login (pending or finished); ok is false when none was started since mam began.
func (s *Service) CurrentLogin() (Login, bool) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if s.login == nil {
		return Login{}, false
	}
	return s.login.info, true
}

// LoginStatus returns the login with id; ok is false for an unknown id.
func (s *Service) LoginStatus(id string) (Login, bool) {
	if l, ok := s.CurrentLogin(); ok && l.ID == id {
		return l, true
	}
	return Login{}, false
}

// CancelLogin stops the login with id when it is pending.
func (s *Service) CancelLogin(ctx context.Context, id string) {
	l, ok := s.CurrentLogin()
	if !ok || l.ID != id || l.State != LoginPending {
		return
	}
	s.finishLogin(id, LoginCancelled, "ログインを中止しました")
	s.cancelRemote(ctx, id)
}

// cancelCurrent cancels the pending login (on the app-server too) before another one starts.
func (s *Service) cancelCurrent(se *appserver.Session, why string) {
	if l, ok := s.CurrentLogin(); ok && l.State == LoginPending {
		s.finishLogin(l.ID, LoginCancelled, why)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = se.Call(ctx, protocol.MethodLoginCancel, protocol.CancelLoginParams{LoginID: l.ID}, nil)
	}
}

// finishLogin moves the pending login with id to a final state. It reports whether it changed anything.
func (s *Service) finishLogin(id string, state LoginState, msg string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	a := s.login
	if a == nil || a.info.ID != id || a.info.State != LoginPending {
		return false
	}
	a.timer.Stop()
	a.info.State, a.info.Message, a.info.FinishedAt = state, msg, s.now()
	return true
}

// expire ends a login nobody finished.
func (s *Service) expire(id string) {
	if s.finishLogin(id, LoginFailed, loginTimeoutMessage) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.cancelRemote(ctx, id)
	}
}

// onNotification handles the account notifications of the app-server. It runs on the reading goroutine: the work that
// calls the app-server is started in a goroutine.
func (s *Service) onNotification(method string, params json.RawMessage) {
	switch method {
	case protocol.NotifLoginCompleted:
		var n protocol.LoginCompleted
		if json.Unmarshal(params, &n) != nil {
			return
		}
		s.loginCompleted(n)
	case protocol.NotifAccountUpdated:
		var n protocol.AccountUpdated
		if json.Unmarshal(params, &n) != nil {
			return
		}
		if n.AuthMode == nil { // a logout (here or by the app-server)
			s.setAccount(AccountView{SignedIn: false})
			return
		}
		s.goRefreshAccount()
	case protocol.NotifRateLimits:
		var n protocol.RateLimitsUpdated
		if json.Unmarshal(params, &n) != nil {
			return
		}
		s.setRateLimits(n.RateLimits)
	}
}

func (s *Service) loginCompleted(n protocol.LoginCompleted) {
	cur, ok := s.CurrentLogin()
	if !ok || cur.State != LoginPending || (n.LoginID != nil && *n.LoginID != cur.ID) {
		return
	}
	if n.Success {
		if s.finishLogin(cur.ID, LoginSucceeded, "ログインしました") {
			s.goRefreshAccount() // the e-mail address, the plan and the usage limit
		}
		return
	}
	msg := "ログインを完了できませんでした"
	if n.Error != nil && *n.Error != "" {
		msg += ": " + sanitize(*n.Error)
	}
	s.finishLogin(cur.ID, LoginFailed, msg)
}

// refreshAccountQuietly reads the account and the limits for the cache; a failure is only logged. It runs in the
// background (Service.bg) on the process that sent the notification and never starts one: after a Stop (a logout)
// there is nothing to read and nothing to write.
func (s *Service) refreshAccountQuietly(se *appserver.Session) {
	defer s.bg.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.refreshOn(ctx, se); err != nil {
		s.opts.Logger.Warn("codex: reading the account failed", "err", sanitize(err.Error()))
	}
}

// goRefreshAccount starts refreshAccountQuietly on the running process (none: nothing to refresh).
func (s *Service) goRefreshAccount() {
	se := s.srv.Current()
	if se == nil {
		return
	}
	s.bg.Add(1)
	go s.refreshAccountQuietly(se)
}

// cancelRemote tells the running app-server to drop the login (nothing is started for it).
func (s *Service) cancelRemote(ctx context.Context, id string) {
	if se := s.srv.Current(); se != nil {
		_ = se.Call(ctx, protocol.MethodLoginCancel, protocol.CancelLoginParams{LoginID: id}, nil)
	}
}
