package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/oauth"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/tokenstore"
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

// browserLoginTimeout is how long a browser login waits for the callback.
const browserLoginTimeout = 10 * time.Minute

// Login is one login attempt as the screen and the MCP tools show it. It holds no token.
type Login struct {
	ID     string
	Method LoginMethod
	State  LoginState
	// Message says what the user should do while pending, or why the login failed.
	Message string
	// AuthURL is the authorization URL of a browser login: opened by mam on its own machine, shown as a fallback.
	AuthURL string
	// BrowserOpened is whether mam started the default browser.
	BrowserOpened bool
	// VerificationURL and UserCode are what the user enters on another device (device login).
	VerificationURL string
	UserCode        string
	Interval        time.Duration
	ExpiresAt       time.Time
	// FinishedAt is when the login left pending (zero while pending).
	FinishedAt time.Time
}

type activeLogin struct {
	info   Login
	cancel context.CancelFunc
}

// start registers a new login, cancelling the one in progress (the loopback port and the user's attention are single).
func (a *Account) start(method LoginMethod, cancel context.CancelFunc) (id string) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.login != nil && a.login.info.State == LoginPending {
		a.login.cancel()
		a.login.info.State, a.login.info.Message, a.login.info.FinishedAt = LoginCancelled, "新しいログインを始めたため中止しました", a.now()
	}
	id = uuid.NewString()
	a.login = &activeLogin{info: Login{ID: id, Method: method, State: LoginPending}, cancel: cancel}
	return id
}

func (a *Account) update(id string, f func(*Login)) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.login != nil && a.login.info.ID == id && a.login.info.State == LoginPending {
		f(&a.login.info)
	}
}

func (a *Account) finish(id string, err error) {
	a.update(id, func(l *Login) {
		l.FinishedAt = a.now()
		if err == nil {
			l.State, l.Message = LoginSucceeded, "ログインしました"
			return
		}
		l.State = LoginFailed
		l.Message = loginFailure(err)
	})
	if err != nil {
		a.logger.Warn("chatgpt login failed", "kind", a.kind, "err", err)
	}
}

// loginFailure is the text the user sees for a failed login. Errors of this package carry no token.
func loginFailure(err error) string {
	switch {
	case errors.Is(err, oauth.ErrAccessDenied):
		return "ChatGPT の画面でログインが許可されませんでした"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, oauth.ErrDeviceExpired):
		return "時間内にログインが完了しませんでした。もう一度始めてください"
	}
	return "ログインを完了できませんでした: " + err.Error()
}

// LoginStatus returns the login with id (the latest one of the account). ok is false for an unknown id.
func (a *Account) LoginStatus(id string) (Login, bool) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.login == nil || a.login.info.ID != id {
		return Login{}, false
	}
	return a.login.info, true
}

// CurrentLogin is the latest login of the account (pending or finished); ok is false when none was started since
// the app began or the last logout.
func (a *Account) CurrentLogin() (Login, bool) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.login == nil {
		return Login{}, false
	}
	return a.login.info, true
}

// CancelLogin stops a pending login.
func (a *Account) CancelLogin(id string) {
	a.update(id, func(l *Login) {
		l.State, l.Message, l.FinishedAt = LoginCancelled, "ログインを中止しました", a.now()
	})
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.login != nil && a.login.info.ID == id {
		a.login.cancel()
	}
}

// StartBrowserLogin starts the authorization-code login: the loopback listener first, then the default browser. The
// login runs in the background; poll LoginStatus. A busy port 1455 is replaced by the next one.
func (a *Account) StartBrowserLogin() (Login, error) {
	reg, err := a.registration()
	if err != nil {
		return Login{}, err
	}
	p := a.profile(reg.ClientID)
	att, err := oauth.NewAttempt()
	if err != nil {
		return Login{}, err
	}
	cb, err := oauth.Listen(p.Ports, att.State)
	if err != nil {
		return Login{}, fmt.Errorf("chatgpt: ログインの待ち受けを始められません: %w", err)
	}
	req := oauth.AuthorizeRequest{Attempt: att, RedirectURI: cb.RedirectURI(), HostID: reg.HostID}
	if t, err := a.store.Load(); err == nil {
		req.LoginHint = t.Meta.Email
	}
	authURL := p.BuildAuthorizeURL(req)

	ctx, cancel := context.WithTimeout(context.Background(), browserLoginTimeout)
	id := a.start(MethodBrowser, cancel)
	a.update(id, func(l *Login) {
		l.AuthURL, l.ExpiresAt = authURL, a.now().Add(browserLoginTimeout)
		l.Message = "ブラウザで ChatGPT にログインしてください"
	})
	if a.open != nil {
		if err := a.open(authURL); err != nil {
			a.logger.Warn("chatgpt login: could not open the browser", "kind", a.kind, "err", err)
			a.update(id, func(l *Login) {
				l.Message = "ブラウザを開けませんでした。表示した URL をこの PC のブラウザで開いてください"
			})
		} else {
			a.update(id, func(l *Login) { l.BrowserOpened = true })
		}
	}
	go a.runBrowser(ctx, cancel, id, cb, p, att, req.RedirectURI, reg)
	login, _ := a.LoginStatus(id)
	return login, nil
}

func (a *Account) runBrowser(ctx context.Context, cancel context.CancelFunc, id string, cb *oauth.Callback, p oauth.Profile, att oauth.Attempt, redirect string, reg tokenstore.Registration) {
	defer cancel()
	defer cb.Close()
	res, err := cb.Wait(ctx)
	if err == nil {
		err = a.complete(ctx, grant{
			profile: p, clientID: pickClient(p, res.ClientID), reported: res.ClientID, code: res.Code, verifier: att.Verifier,
			redirectURI: redirect, nonce: att.Nonce, reg: reg,
		})
	}
	a.finish(id, err)
}

// pickClient is the client of the code exchange: the issued id the callback of a registration returns, else the one of the profile.
func pickClient(p oauth.Profile, fromCallback string) string {
	if p.ClientID == "" {
		return fromCallback
	}
	return p.ClientID
}

// registration returns the stored registration with a host id (created once for the official login).
func (a *Account) registration() (tokenstore.Registration, error) {
	reg, err := a.store.Registration()
	if err != nil {
		return reg, fmt.Errorf("chatgpt: read the registration: %w", err)
	}
	if a.kind == KindOfficial && reg.HostID == "" {
		reg.HostID = "urn:uuid:" + uuid.NewString()
		if err := a.store.SaveRegistration(reg); err != nil {
			return reg, fmt.Errorf("chatgpt: store the registration: %w", err)
		}
	}
	return reg, nil
}

// grant is what a login hands to complete.
type grant struct {
	profile     oauth.Profile
	clientID    string
	reported    string // client_id the callback reported
	code        string
	verifier    string
	redirectURI string
	nonce       string
	reg         tokenstore.Registration
}

// complete exchanges the code, validates the identity and stores the sign-in. It runs under the refresh lock: a
// sign-in replaces tokens another process may be refreshing.
func (a *Account) complete(ctx context.Context, g grant) error {
	if g.clientID == "" || g.clientID == oauth.DynamicClientID {
		return errors.New("登録が完了していません（クライアント ID が返されませんでした）")
	}
	if g.profile.ClientID != "" && g.reported != "" && g.reported != g.profile.ClientID {
		return errors.New("別のクライアント ID が返されたため、ログインを破棄しました")
	}
	resp, err := a.client.Exchange(ctx, g.profile, g.clientID, g.code, g.verifier, g.redirectURI)
	if err != nil {
		return err
	}
	var claims oauth.IDClaims
	switch {
	case g.profile.VerifyIDToken:
		if resp.IDToken == "" {
			return errors.New("ID トークンが返されませんでした")
		}
		if claims, err = a.client.VerifyIDToken(ctx, g.profile, resp.IDToken, g.clientID, g.nonce); err != nil {
			return err
		}
	case resp.IDToken != "":
		claims, _ = oauth.UnverifiedClaims(resp.IDToken) // a label and a header: a token that cannot be read has none
	}
	scopes := resp.Scopes()
	return a.withLock(ctx, func() error {
		if prev, err := a.store.Load(); err == nil && g.reg.ClientID != "" && prev.Meta.Subject != "" && claims.Subject != "" && prev.Meta.Subject != claims.Subject {
			return errors.New("保存済みのログインと別のアカウントです。いったんログアウトしてからログインしてください")
		}
		if a.kind == KindOfficial {
			reg := g.reg
			reg.ClientID = g.clientID
			if err := a.store.SaveRegistration(reg); err != nil {
				return fmt.Errorf("store the registration: %w", err)
			}
		}
		return a.store.Save(tokenstore.Tokens{
			Access: resp.AccessToken, Refresh: resp.RefreshToken, IDToken: resp.IDToken,
			Meta: tokenstore.Meta{
				ClientID: g.clientID, ExpiresAt: a.expiry(resp), Scopes: slices.Clone(scopes), Email: claims.Email,
				Plan: claims.Plan, AccountID: claims.AccountID, Subject: claims.Subject, SavedAt: a.now(),
			},
		})
	})
}
