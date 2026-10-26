// Package chatgpttest is a fake auth.openai.com for tests: both clients' authorize-less endpoints (token, refresh,
// revoke, device code, JWKS, discovery) over httptest, and the browser of a user who approves a sign-in.
package chatgpttest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// Client ids the fake server knows.
const (
	IssuedClient = "oaiapp_test_client"
	CodexClient  = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// Server is a stand-in for auth.openai.com that serves both clients.
type Server struct {
	*httptest.Server
	t   *testing.T
	key *rsa.PrivateKey

	mu    sync.Mutex
	nonce string
	seq   int
	reqs  map[string]int
	polls int

	// RevokeStatus is the status of the revocation endpoints (0: 200).
	RevokeStatus int
	// RefreshHandler may answer a refresh itself: it returns true when it did.
	RefreshHandler func(w http.ResponseWriter, form url.Values) bool
	// PendingPolls is how many device polls are answered "pending" first.
	PendingPolls int
	// AccessLen makes the access tokens long (the SIWC ones may exceed an OS entry).
	AccessLen int
	// RefreshDelay slows a refresh down (to let callers pile up).
	RefreshDelay time.Duration
}

// NewServer starts the fake server; it stops with the test.
func NewServer(t *testing.T) *Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	a := &Server{t: t, key: key, reqs: map[string]int{}, AccessLen: 2800}
	a.Server = httptest.NewServer(http.HandlerFunc(a.serve))
	t.Cleanup(a.Close)
	return a
}

// Count is how many requests the path got.
func (a *Server) Count(path string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reqs[path]
}

// Total is how many requests the server got.
func (a *Server) Total() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, c := range a.reqs {
		n += c
	}
	return n
}

func (a *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	form := url.Values{}
	var js map[string]any
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(body, &js)
	} else {
		form, _ = url.ParseQuery(string(body))
	}
	a.mu.Lock()
	a.reqs[r.URL.Path]++
	a.mu.Unlock()
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch r.URL.Path {
	case "/.well-known/jwks.json":
		reply(200, map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "kid": "k1", "n": base64.RawURLEncoding.EncodeToString(a.key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(a.key.E)).Bytes()),
		}}})
	case "/.well-known/openid-configuration":
		reply(200, map[string]any{"revocation_endpoint": a.URL + "/api/accounts/oauth/revoke"})
	case "/api/accounts/oauth/revoke", "/oauth/revoke":
		if a.RevokeStatus != 0 {
			w.WriteHeader(a.RevokeStatus)
			return
		}
		w.WriteHeader(200)
	case "/api/accounts/oauth/token":
		a.officialToken(w, form, reply)
	case "/oauth/token":
		a.codexToken(w, form, js, reply)
	case "/api/accounts/deviceauth/usercode":
		reply(200, map[string]any{"device_auth_id": "dev-1", "user_code": "ABCD-1234", "interval": "1"})
	case "/api/accounts/deviceauth/token":
		a.mu.Lock()
		a.polls++
		pending := a.polls <= a.PendingPolls
		a.mu.Unlock()
		if pending {
			reply(403, map[string]any{"error": "pending"})
			return
		}
		reply(200, map[string]any{"authorization_code": "dev-code", "code_challenge": "c", "code_verifier": "dev-verifier"})
	default:
		http.NotFound(w, r)
	}
}

// IDToken is a signed ID token of the server.
func (a *Server) IDToken(aud, nonce, email string) string {
	claims := jwt.MapClaims{
		"iss": a.URL, "aud": aud, "sub": "user-1", "email": email, "nonce": nonce,
		"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(48 * time.Hour).Unix(),
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct-1", "chatgpt_plan_type": "plus"},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	raw, err := tok.SignedString(a.key)
	require.NoError(a.t, err)
	return raw
}

func (a *Server) newTokens(prefix string) (access, refresh string) {
	a.mu.Lock()
	a.seq++
	n := a.seq
	a.mu.Unlock()
	access = prefix + "-ACCESS-" + strings.Repeat("x", a.AccessLen) + "-" + string(rune('0'+n%10))
	refresh = prefix + "-REFRESH-" + string(rune('0'+n%10)) + "-secretsecret"
	return access, refresh
}

func (a *Server) officialToken(w http.ResponseWriter, form url.Values, reply func(int, any)) {
	switch form.Get("grant_type") {
	case "authorization_code":
		a.mu.Lock()
		nonce := a.nonce
		a.mu.Unlock()
		access, refresh := a.newTokens("off")
		reply(200, map[string]any{
			"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": 3600,
			"id_token": a.IDToken(form.Get("client_id"), nonce, "me@example.com"),
			"scope":    "chatgpt.tokens.use.direct email offline_access openid profile resource.invoke",
		})
	case "refresh_token":
		time.Sleep(a.RefreshDelay)
		if a.RefreshHandler != nil && a.RefreshHandler(w, form) {
			return
		}
		access, refresh := a.newTokens("off")
		reply(200, map[string]any{"access_token": access, "refresh_token": refresh, "expires_in": 3600})
	default:
		reply(400, map[string]any{"error": "unsupported_grant_type"})
	}
}

func (a *Server) codexToken(w http.ResponseWriter, form url.Values, js map[string]any, reply func(int, any)) {
	if js != nil && js["grant_type"] == "refresh_token" {
		time.Sleep(a.RefreshDelay)
		if a.RefreshHandler != nil && a.RefreshHandler(w, form) {
			return
		}
		access, refresh := a.newTokens("cdx")
		reply(200, map[string]any{"access_token": access, "refresh_token": refresh, "expires_in": 3600})
		return
	}
	access, refresh := a.newTokens("cdx")
	reply(200, map[string]any{
		"access_token": access, "refresh_token": refresh, "expires_in": 3600,
		"id_token": a.IDToken(CodexClient, "", "codex@example.com"), "scope": "openid profile email offline_access",
	})
}

// Approve is the browser of a user who signs in: it follows the redirect of the authorization URL to the loopback
// callback with a code (and the issued client id of a first registration).
func (a *Server) Approve(authURL string) {
	u, err := url.Parse(authURL)
	require.NoError(a.t, err)
	q := u.Query()
	a.mu.Lock()
	a.nonce = q.Get("nonce")
	a.mu.Unlock()
	cb, err := url.Parse(q.Get("redirect_uri"))
	require.NoError(a.t, err)
	cbq := url.Values{"code": {"the-code"}, "state": {q.Get("state")}}
	if q.Get("client_id") == "dynamic_agent_client" {
		cbq.Set("client_id", IssuedClient)
	}
	go func() {
		res, err := http.Get(cb.String() + "?" + cbq.Encode()) //nolint:gosec,noctx // the user's browser, a test
		if err == nil {
			_ = res.Body.Close()
		}
	}()
}
