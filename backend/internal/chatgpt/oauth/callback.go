package oauth

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ErrAccessDenied is an authorization the user refused (callback error=access_denied).
var ErrAccessDenied = errors.New("oauth: authorization was denied")

// PolicyDeniedCode is the error the authorization server returns to a workspace account (Business / Enterprise /
// Edu) whose organization does not allow the app: OpenAI shows "あなたの組織では、このアプリによるアクセスが許可されていません".
const PolicyDeniedCode = "3p_delegated_access_policy_denied"

// ServerError is an authorization the server refused with an error code (callback error=<code>).
type ServerError struct{ Code string }

func (e *ServerError) Error() string {
	return fmt.Sprintf("oauth: the authorization server returned %q", e.Code)
}

// CallbackResult is a callback with the right state.
type CallbackResult struct {
	Code string
	// ClientID is the issued client id of a new registration (empty on a reauthorization).
	ClientID string
	// Scope is the space-separated scope the callback reports (the token response is what decides).
	Scope string
}

// Callback is the loopback listener of one authorization attempt. It is started before the browser opens.
type Callback struct {
	ln    net.Listener
	srv   *http.Server
	state string
	port  int

	once   sync.Once
	result chan callbackOutcome
}

type callbackOutcome struct {
	res CallbackResult
	err error
}

// Listen binds 127.0.0.1 on the first free port of ports (0: any free port). A busy port is skipped.
func Listen(ports []int, state string) (*Callback, error) {
	var lastErr error
	for _, p := range ports {
		ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if err != nil {
			lastErr = err
			continue
		}
		c := &Callback{ln: ln, state: state, port: ln.Addr().(*net.TCPAddr).Port, result: make(chan callbackOutcome, 1)}
		c.srv = &http.Server{Handler: http.HandlerFunc(c.handle), ReadHeaderTimeout: 10 * time.Second}
		go func() { _ = c.srv.Serve(ln) }()
		return c, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no port to try")
	}
	return nil, fmt.Errorf("oauth: no free callback port: %w", lastErr)
}

// Port is the port the listener is bound to.
func (c *Callback) Port() int { return c.port }

// RedirectURI is the redirect_uri of the attempt.
func (c *Callback) RedirectURI() string { return RedirectURI(c.port) }

// Wait returns the callback with the right state, ErrAccessDenied for a refusal, or ctx.Err(). A request with
// another state is answered with an error page and dropped: it does not end the attempt.
func (c *Callback) Wait(ctx context.Context) (CallbackResult, error) {
	select {
	case o := <-c.result:
		return o.res, o.err
	case <-ctx.Done():
		return CallbackResult{}, ctx.Err()
	}
}

// Close stops the listener. It is safe to call more than once.
func (c *Callback) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = c.srv.Shutdown(ctx)
}

func (c *Callback) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	// A page of another origin must not be able to reach the callback through a rebinding of its host name.
	if host, _, err := net.SplitHostPort(r.Host); err != nil || host != "127.0.0.1" {
		http.Error(w, "bad host", http.StatusBadRequest)
		return
	}
	if r.URL.Path != callbackPath || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	if q.Get("state") != c.state {
		page(w, http.StatusBadRequest, "ログインに失敗しました", "この要求は、mam が開始したログインのものではありません。mam の画面からやり直してください。")
		return
	}
	var out callbackOutcome
	switch {
	case q.Get("error") == "access_denied":
		out.err = ErrAccessDenied
	case q.Get("error") != "":
		out.err = &ServerError{Code: q.Get("error")}
	case q.Get("code") == "":
		out.err = errors.New("oauth: the callback has no code")
	default:
		out.res = CallbackResult{Code: q.Get("code"), ClientID: q.Get("client_id"), Scope: q.Get("scope")}
	}
	delivered := false
	c.once.Do(func() { c.result <- out; delivered = true })
	if !delivered {
		page(w, http.StatusConflict, "このログインは終了しています", "mam の画面を確認してください。")
		return
	}
	if out.err != nil {
		page(w, http.StatusOK, "ログインを完了できませんでした", "mam の画面に戻って、表示を確認してください。")
		return
	}
	page(w, http.StatusOK, "ログインしました", "このタブは閉じて、mam の画面に戻ってください。")
}

func page(w http.ResponseWriter, status int, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<!doctype html><meta charset=utf-8><title>%s</title><body style=\"font-family:sans-serif;margin:3em\"><h1>%s</h1><p>%s</p>",
		html.EscapeString(title), html.EscapeString(title), html.EscapeString(body))
}
