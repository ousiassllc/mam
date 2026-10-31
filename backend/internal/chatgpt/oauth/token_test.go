package oauth_test

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/oauth"
)

func TestExchangeAndRefresh_OfficialUsesFormsWithTheResourceAndCodexUsesJSON(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, _ request) {
		writeJSON(w, 200, map[string]any{"access_token": "at-1", "refresh_token": "rt-2", "id_token": "id", "expires_in": 3600, "scope": "openid email"})
	})
	c, _ := testClient(t)
	official, codex := oauth.Official(srv.URL, "oaiapp_1"), oauth.Codex(srv.URL)

	tok, err := c.Exchange(context.Background(), official, "oaiapp_1", "the-code", "the-verifier", "http://127.0.0.1:1455/auth/callback")
	require.NoError(t, err)
	assert.Equal(t, "at-1", tok.AccessToken)
	assert.Equal(t, []string{"openid", "email"}, tok.Scopes())
	ex := srv.requests("/api/accounts/oauth/token")[0]
	assert.Equal(t, "authorization_code", ex.Form.Get("grant_type"))
	assert.Equal(t, "oaiapp_1", ex.Form.Get("client_id"))
	assert.Equal(t, "the-verifier", ex.Form.Get("code_verifier"))
	assert.Equal(t, "http://127.0.0.1:1455/auth/callback", ex.Form.Get("redirect_uri"))
	assert.Equal(t, "https://api.openai.com/v1", ex.Form.Get("resource"))

	_, err = c.Refresh(context.Background(), official, "oaiapp_1", "rt-1")
	require.NoError(t, err)
	rf := srv.requests("/api/accounts/oauth/token")[1]
	assert.Equal(t, "refresh_token", rf.Form.Get("grant_type"))
	assert.Equal(t, "rt-1", rf.Form.Get("refresh_token"))
	assert.Equal(t, "https://api.openai.com/v1", rf.Form.Get("resource"))
	assert.Empty(t, rf.Form.Get("scope"), "the grant is kept")

	_, err = c.Refresh(context.Background(), codex, oauth.CodexClientID, "rt-1")
	require.NoError(t, err)
	cr := srv.requests("/oauth/token")[0]
	assert.Equal(t, "application/json", cr.ContentType)
	assert.Equal(t, "refresh_token", cr.JSON["grant_type"])
	assert.Equal(t, oauth.CodexClientID, cr.JSON["client_id"])
}

func TestReloginRequired_ClassificationTable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
		code   string
	}{
		{"invalid_grant string", 400, `{"error":"invalid_grant"}`, true, "invalid_grant"},
		{"invalid_refresh_token", 401, `{"error":"invalid_refresh_token"}`, true, "invalid_refresh_token"},
		{"token_expired", 401, `{"error":{"code":"token_expired"}}`, true, "token_expired"},
		{"refresh_token_expired", 401, `{"error":{"code":"refresh_token_expired","message":"x"}}`, true, "refresh_token_expired"},
		{"refresh_token_invalidated", 401, `{"code":"refresh_token_invalidated"}`, true, "refresh_token_invalidated"},
		{"refresh_token_reused", 400, `{"error":"refresh_token_reused"}`, true, "refresh_token_reused"},
		{"invalid_client is a configuration bug", 400, `{"error":"invalid_client"}`, false, ""},
		{"unknown 4xx keeps the tokens", 403, `{"error":"something_new"}`, false, ""},
		{"a 5xx never ends the sign-in, even with the code", 503, `{"error":"invalid_grant"}`, false, ""},
		{"429", 429, `{"error":"rate_limited"}`, false, ""},
		{"no body", 400, ``, false, ""},
	}
	c, _ := testClient(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, _ request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := c.Refresh(context.Background(), oauth.Codex(srv.URL), oauth.CodexClientID, "rt")
			require.Error(t, err)
			code, ok := oauth.ReloginRequired(err)
			assert.Equal(t, tc.want, ok)
			assert.Equal(t, tc.code, code)
		})
	}

	t.Run("a network failure is never a reason to log in again", func(t *testing.T) {
		// A real client whose dial is refused. Dialing the port of a closed server would reach whatever another
		// process of the host (a parallel test) listens on by then.
		refused := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
		c, _ := testClient(t)
		c.HTTP = &http.Client{Transport: &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) { return nil, refused },
		}}
		issuer := "http://auth.example.invalid:4711"
		_, err := c.Refresh(context.Background(), oauth.Codex(issuer), oauth.CodexClientID, "SECRET-REFRESH-TOKEN")
		require.Error(t, err)
		_, ok := oauth.ReloginRequired(err)
		assert.False(t, ok)
		var ne *oauth.NetworkError
		assert.ErrorAs(t, err, &ne)
		assert.ErrorIs(t, err, syscall.ECONNREFUSED)
		assert.NotContains(t, err.Error(), "SECRET-REFRESH-TOKEN")
		assert.NotContains(t, err.Error(), issuer, "the URL is dropped from a transport error")
	})
}

func TestRevoke_OfficialFollowsDiscoveryAndCodexSendsJSON(t *testing.T) {
	var srv *fakeServer
	srv = newFakeServer(t, func(w http.ResponseWriter, r request) {
		switch r.Path {
		case "/.well-known/openid-configuration":
			writeJSON(w, 200, map[string]any{"revocation_endpoint": srv.URL + "/discovered/revoke"})
		default:
			w.WriteHeader(200)
		}
	})
	c, _ := testClient(t)

	require.NoError(t, c.Revoke(context.Background(), oauth.Official(srv.URL, "oaiapp_1"), "oaiapp_1", "rt-1"))
	got := srv.requests("/discovered/revoke")
	require.Len(t, got, 1)
	assert.Equal(t, "rt-1", got[0].Form.Get("token"))
	assert.Equal(t, "refresh_token", got[0].Form.Get("token_type_hint"))
	assert.Equal(t, "oaiapp_1", got[0].Form.Get("client_id"))

	require.NoError(t, c.Revoke(context.Background(), oauth.Codex(srv.URL), oauth.CodexClientID, "rt-1"))
	got = srv.requests("/oauth/revoke")
	require.Len(t, got, 1)
	assert.Equal(t, "application/json", got[0].ContentType)
	assert.Equal(t, "rt-1", got[0].JSON["token"])
	assert.Equal(t, "refresh_token", got[0].JSON["token_type_hint"])
}

func TestRevoke_RefusesADiscoveredEndpointOfAnotherHostAndReportsFailure(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r request) {
		if r.Path == "/.well-known/openid-configuration" {
			writeJSON(w, 200, map[string]any{"revocation_endpoint": "https://evil.example/revoke"})
			return
		}
		w.WriteHeader(500)
	})
	c, _ := testClient(t)
	err := c.Revoke(context.Background(), oauth.Official(srv.URL, "oaiapp_1"), "oaiapp_1", "rt-1")
	var te *oauth.TokenError
	require.ErrorAs(t, err, &te, "the refresh token went to the profile's own endpoint, which failed")
	assert.Equal(t, 500, te.Status)
	assert.Len(t, srv.requests("/api/accounts/oauth/revoke"), 1)
}

func deviceServer(t *testing.T, pendingPolls int) *fakeServer {
	var polls atomic.Int64 // handlers of different connections run on different goroutines
	return newFakeServer(t, func(w http.ResponseWriter, r request) {
		switch r.Path {
		case "/api/accounts/deviceauth/usercode":
			writeJSON(w, 200, map[string]any{"device_auth_id": "dev-1", "user_code": "ABCD-1234", "interval": "7", "expires_at": fixedNow.Add(15 * time.Minute).Format(time.RFC3339)})
		case "/api/accounts/deviceauth/token":
			if int(polls.Add(1)) <= pendingPolls {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "deviceauth_authorization_pending"})
				return
			}
			writeJSON(w, 200, map[string]any{"authorization_code": "auth-code", "code_challenge": "ch", "code_verifier": "ver"})
		}
	})
}

func TestDevice_RequestsACodeAndPollsAtTheServersInterval(t *testing.T) {
	srv := deviceServer(t, 2)
	c, slept := testClient(t)
	p := oauth.Codex(srv.URL)

	dc, err := c.RequestDeviceCode(context.Background(), p)
	require.NoError(t, err)
	assert.Equal(t, "ABCD-1234", dc.UserCode)
	assert.Equal(t, srv.URL+"/codex/device", dc.VerificationURL)
	assert.Equal(t, 7*time.Second, dc.Interval, "the interval arrives as a string")
	assert.Equal(t, fixedNow.Add(15*time.Minute), dc.ExpiresAt)
	assert.Equal(t, oauth.CodexClientID, srv.requests("/api/accounts/deviceauth/usercode")[0].JSON["client_id"])

	grant, err := c.PollDevice(context.Background(), p, dc)
	require.NoError(t, err)
	assert.Equal(t, oauth.DeviceGrant{Code: "auth-code", Verifier: "ver"}, grant)
	assert.Equal(t, []time.Duration{7 * time.Second, 7 * time.Second}, *slept, "two pending answers, each waited out at the interval")
	poll := srv.requests("/api/accounts/deviceauth/token")[0]
	assert.Equal(t, "dev-1", poll.JSON["device_auth_id"])
	assert.Equal(t, "ABCD-1234", poll.JSON["user_code"])
}

func TestDevice_StopsWhenTheCodeExpiresAndOnHardErrors(t *testing.T) {
	srv := deviceServer(t, 1_000_000)
	c, slept := testClient(t)
	p := oauth.Codex(srv.URL)
	dc, err := c.RequestDeviceCode(context.Background(), p)
	require.NoError(t, err)

	_, err = c.PollDevice(context.Background(), p, dc)
	require.ErrorIs(t, err, oauth.ErrDeviceExpired)
	var total time.Duration
	for _, d := range *slept {
		assert.LessOrEqual(t, d, 7*time.Second)
		total += d
	}
	assert.LessOrEqual(t, total, 15*time.Minute, "never waits past the expiry")

	c, _ = testClient(t) // the clock of the first client is past the expiry now
	hard := newFakeServer(t, func(w http.ResponseWriter, _ request) { writeJSON(w, 400, map[string]any{"error": "bad_request"}) })
	_, err = c.PollDevice(context.Background(), oauth.Codex(hard.URL), dc)
	var te *oauth.TokenError
	require.ErrorAs(t, err, &te)
	assert.Equal(t, 400, te.Status)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	_, err = c.PollDevice(ctx, p, dc)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestDevice_TheOfficialClientHasNone(t *testing.T) {
	srv := newFakeServer(t, func(http.ResponseWriter, request) { t.Error("no request expected") })
	c, _ := testClient(t)
	_, err := c.RequestDeviceCode(context.Background(), oauth.Official(srv.URL, ""))
	require.ErrorIs(t, err, oauth.ErrNoDeviceCode)

	nf := newFakeServer(t, func(w http.ResponseWriter, _ request) { w.WriteHeader(404) })
	_, err = c.RequestDeviceCode(context.Background(), oauth.Codex(nf.URL))
	require.ErrorIs(t, err, oauth.ErrNoDeviceCode)
}
