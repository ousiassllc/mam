package oauth_test

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/oauth"
)

func TestAttempt_ChallengeIsTheS256OfTheVerifierAndEveryAttemptIsNew(t *testing.T) {
	a, err := oauth.NewAttempt()
	require.NoError(t, err)
	b, err := oauth.NewAttempt()
	require.NoError(t, err)
	assert.NotEqual(t, a.State, b.State)
	assert.NotEqual(t, a.Nonce, b.Nonce)
	assert.NotEqual(t, a.Verifier, b.Verifier)
	assert.GreaterOrEqual(t, len(a.Verifier), 43, "RFC 7636 verifier length")
	assert.LessOrEqual(t, len(a.Verifier), 128)

	// The example of RFC 7636 appendix B.
	rfc := oauth.Attempt{Verifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"}
	assert.Equal(t, "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", rfc.Challenge())
}

func authorizeQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u.Query()
}

func TestBuildAuthorizeURL_OfficialRegistrationReauthorizationAndCodex(t *testing.T) {
	att := oauth.Attempt{State: "st", Nonce: "no", Verifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"}
	req := oauth.AuthorizeRequest{Attempt: att, RedirectURI: oauth.RedirectURI(1455), HostID: "urn:uuid:host"}

	first := oauth.Official("", "").BuildAuthorizeURL(req)
	assert.Contains(t, first, "https://auth.openai.com/api/accounts/authorize?")
	q := authorizeQuery(t, first)
	assert.Equal(t, "dynamic_agent_client", q.Get("client_id"))
	assert.Equal(t, "mam", q.Get("agent_name_hint"))
	assert.Equal(t, "urn:uuid:host", q.Get("ext_agent_host_id"))
	assert.Equal(t, "http://127.0.0.1:1455/auth/callback", q.Get("redirect_uri"), "127.0.0.1, never localhost")
	assert.Equal(t, "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct", q.Get("scope"))
	assert.Equal(t, "https://api.openai.com/v1", q.Get("resource"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", q.Get("code_challenge"))
	assert.Equal(t, "st", q.Get("state"))
	assert.Equal(t, "no", q.Get("nonce"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Empty(t, q.Get("id_token_hint"), "an ID token is never put in a URL")

	req.LoginHint = "me@example.com"
	q = authorizeQuery(t, oauth.Official("", "oaiapp_issued").BuildAuthorizeURL(req))
	assert.Equal(t, "oaiapp_issued", q.Get("client_id"), "a returning sign-in uses the issued id")
	assert.Empty(t, q.Get("agent_name_hint"), "the name is sent on registration only")
	assert.Equal(t, "me@example.com", q.Get("login_hint"))
	assert.Equal(t, "urn:uuid:host", q.Get("ext_agent_host_id"))

	q = authorizeQuery(t, oauth.Codex("").BuildAuthorizeURL(oauth.AuthorizeRequest{Attempt: att, RedirectURI: oauth.RedirectURI(1457)}))
	assert.Equal(t, oauth.CodexClientID, q.Get("client_id"))
	assert.Equal(t, "openid profile email offline_access api.connectors.read api.connectors.invoke", q.Get("scope"))
	assert.Equal(t, "true", q.Get("codex_cli_simplified_flow"))
	assert.Empty(t, q.Get("resource"))
	assert.Empty(t, q.Get("nonce"), "the Codex flow sends none")
	assert.Equal(t, "http://127.0.0.1:1457/auth/callback", q.Get("redirect_uri"))
}

func get(t *testing.T, target string) *http.Response {
	t.Helper()
	res, err := http.Get(target) //nolint:gosec,noctx // a test request to the loopback listener under test
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// The ports the tests of the callers replace (chatgpt.Options.CallbackPorts) stay what the registrations allow.
func TestProfiles_CallbackPortsAreTheRegisteredOnes(t *testing.T) {
	assert.Equal(t, []int{1455, 1457, 0}, oauth.Official("", "").Ports, "official: 1455, 1457, then any free port")
	assert.Equal(t, []int{1455, 1457}, oauth.Codex("").Ports, "the Codex client allows 1455 and 1457 only")
}

func TestCallback_UsesTheNextPortWhenTheFirstIsBusy(t *testing.T) {
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = busy.Close() }()
	busyPort := busy.Addr().(*net.TCPAddr).Port

	cb, err := oauth.Listen([]int{busyPort, 0}, "st")
	require.NoError(t, err)
	defer cb.Close()
	assert.NotEqual(t, busyPort, cb.Port(), "1455 busy: another port")
	assert.Equal(t, "http://127.0.0.1:"+strconv.Itoa(cb.Port())+"/auth/callback", cb.RedirectURI())

	_, err = oauth.Listen([]int{busyPort}, "st")
	require.Error(t, err, "no port left")
}

func TestCallback_DropsAWrongStateAndDeliversTheRightOne(t *testing.T) {
	cb, err := oauth.Listen([]int{0}, "right")
	require.NoError(t, err)
	defer cb.Close()
	base := "http://127.0.0.1:" + strconv.Itoa(cb.Port()) + "/auth/callback"

	res := get(t, base+"?code=evil&state=wrong")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	assert.Equal(t, http.StatusNotFound, get(t, "http://127.0.0.1:"+strconv.Itoa(cb.Port())+"/other").StatusCode)

	res = get(t, base+"?code=abc&state=right&client_id=oaiapp_1&scope=openid+email")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	got, err := cb.Wait(context.Background())
	require.NoError(t, err)
	assert.Equal(t, oauth.CallbackResult{Code: "abc", ClientID: "oaiapp_1", Scope: "openid email"}, got, "the wrong-state request did not end the attempt")

	assert.Equal(t, http.StatusConflict, get(t, base+"?code=again&state=right").StatusCode, "one result per attempt")
}

func TestCallback_AccessDeniedAndForeignHostAndContext(t *testing.T) {
	cb, err := oauth.Listen([]int{0}, "st")
	require.NoError(t, err)
	defer cb.Close()
	port := strconv.Itoa(cb.Port())

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+port+"/auth/callback?code=x&state=st", nil) //nolint:noctx // test
	require.NoError(t, err)
	req.Host = "evil.example:" + port      // a DNS-rebinding page
	res, err := http.DefaultClient.Do(req) //nolint:gosec // test
	require.NoError(t, err)
	_ = res.Body.Close()
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	get(t, "http://127.0.0.1:"+port+"/auth/callback?error=access_denied&state=st")
	_, err = cb.Wait(context.Background())
	require.ErrorIs(t, err, oauth.ErrAccessDenied)

	cb2, err := oauth.Listen([]int{0}, "st")
	require.NoError(t, err)
	defer cb2.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = cb2.Wait(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
