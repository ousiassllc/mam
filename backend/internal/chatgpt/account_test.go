package chatgpt_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/chatgpttest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/tokenstore"
)

func signInOfficial(t *testing.T, f *fixture) {
	t.Helper()
	l, err := f.svc.Official().StartBrowserLogin()
	require.NoError(t, err)
	got := waitLogin(t, f.svc.Official(), l.ID)
	require.Equal(t, chatgpt.LoginSucceeded, got.State, got.Message)
}

func TestOfficialBrowserLogin_StoresTheSignInAndShowsTheAccount(t *testing.T) {
	f := newFixture(t)
	l, err := f.svc.Official().StartBrowserLogin()
	require.NoError(t, err)
	assert.Equal(t, chatgpt.MethodBrowser, l.Method)
	assert.True(t, l.BrowserOpened)
	assert.Contains(t, l.AuthURL, "dynamic_agent_client", "the first sign-in registers")

	got := waitLogin(t, f.svc.Official(), l.ID)
	require.Equal(t, chatgpt.LoginSucceeded, got.State, got.Message)

	st, err := f.svc.Official().Status()
	require.NoError(t, err)
	assert.True(t, st.SignedIn)
	assert.Equal(t, "me@example.com", st.Email)
	assert.Equal(t, "plus", st.Plan)
	assert.True(t, st.PlanUsageGranted)
	assert.False(t, st.NeedsRelogin)

	// The long tokens (2.8 kB access, id token) went through a store that refuses an entry over 2560 bytes.
	creds, err := f.svc.Official().Credentials(context.Background())
	require.NoError(t, err)
	assert.Contains(t, creds.AccessToken, "off-ACCESS-")
	assert.Equal(t, "acct-1", creds.AccountID)

	// The issued client id is kept: the next sign-in reuses it and does not register again.
	l2, err := f.svc.Official().StartBrowserLogin()
	require.NoError(t, err)
	assert.Contains(t, l2.AuthURL, "client_id="+chatgpttest.IssuedClient)
	assert.NotContains(t, l2.AuthURL, "agent_name_hint")
	assert.Contains(t, l2.AuthURL, "login_hint=me%40example.com")
	assert.Equal(t, chatgpt.LoginSucceeded, waitLogin(t, f.svc.Official(), l2.ID).State)

	// Nothing the screens can see holds a token: the status, the login, the log.
	for _, v := range []any{st, l, l2, got} {
		raw, _ := json.Marshal(v)
		assert.Empty(t, f.leaks(string(raw)))
		assert.Empty(t, f.leaks(fmt.Sprintf("%+v", v)))
	}
	assert.Empty(t, f.leaks(f.logs.String()), "the log")
	assert.Empty(t, f.leaks(fmt.Sprintf("%v %#v %+v", creds, creds, creds)), "Credentials print redacted")
	for _, k := range []string{"access", "refresh", "id_token", "meta"} {
		ok, err := f.keys.Has("chatgpt/official/" + k)
		require.NoError(t, err)
		assert.True(t, ok, k)
	}
}

// The preferred ports (in production 1455 and 1457) are stood in for by ports of the test that stay busy, so the test
// does not depend on what other processes of the host hold.
func TestOfficialBrowserLogin_UsesAnotherPortWhen1455IsBusy(t *testing.T) {
	first, second := busyPort(t), busyPort(t)
	f := newFixtureOn(t, []int{first, second, 0})
	l, err := f.svc.Official().StartBrowserLogin()
	require.NoError(t, err)
	u, err := url.Parse(l.AuthURL)
	require.NoError(t, err)
	redirect, err := url.Parse(u.Query().Get("redirect_uri"))
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", redirect.Hostname())
	assert.NotEqual(t, strconv.Itoa(first), redirect.Port())
	assert.NotEqual(t, strconv.Itoa(second), redirect.Port())
	require.Equal(t, chatgpt.LoginSucceeded, waitLogin(t, f.svc.Official(), l.ID).State, "the login works on the alternative port")
}

func TestBrowserLogin_FailsWhenEveryCallbackPortIsBusy(t *testing.T) {
	f := newFixtureOn(t, []int{busyPort(t), busyPort(t)})
	require.NoError(t, f.svc.SetImageEnabled(true, true))
	_, err := f.svc.Official().StartBrowserLogin()
	require.ErrorContains(t, err, "no free callback port")
	_, err = f.svc.StartImageBrowserLogin()
	require.ErrorContains(t, err, "no free callback port")
}

func TestOfficialBrowserLogin_RefusesAnUnregisteredCallbackAndADifferentIssuedClient(t *testing.T) {
	f := newFixture(t)
	f.browser = func(authURL string) { // the server forgot to return the issued client id
		u, _ := url.Parse(authURL)
		q := u.Query()
		cb, _ := url.Parse(q.Get("redirect_uri"))
		go func() {
			res, err := http.Get(cb.String() + "?code=c&state=" + q.Get("state")) //nolint:gosec,noctx // test
			if err == nil {
				_ = res.Body.Close()
			}
		}()
	}
	l, err := f.svc.Official().StartBrowserLogin()
	require.NoError(t, err)
	got := waitLogin(t, f.svc.Official(), l.ID)
	assert.Equal(t, chatgpt.LoginFailed, got.State)
	assert.Contains(t, got.Message, "登録が完了していません")
	st, _ := f.svc.Official().Status()
	assert.False(t, st.SignedIn)
	assert.Zero(t, f.auth.Count("/api/accounts/oauth/token"), "no code is exchanged without an issued client id")
}

func TestOfficialLogin_AccessDeniedAndCancel(t *testing.T) {
	f := newFixture(t)
	f.browser = func(authURL string) {
		u, _ := url.Parse(authURL)
		q := u.Query()
		cb, _ := url.Parse(q.Get("redirect_uri"))
		go func() {
			res, err := http.Get(cb.String() + "?error=access_denied&state=" + q.Get("state")) //nolint:gosec,noctx // test
			if err == nil {
				_ = res.Body.Close()
			}
		}()
	}
	l, err := f.svc.Official().StartBrowserLogin()
	require.NoError(t, err)
	got := waitLogin(t, f.svc.Official(), l.ID)
	assert.Equal(t, chatgpt.LoginFailed, got.State)
	assert.Contains(t, got.Message, "許可されませんでした")

	f.browser = func(string) {} // the user never comes back
	l, err = f.svc.Official().StartBrowserLogin()
	require.NoError(t, err)
	f.svc.Official().CancelLogin(l.ID)
	assert.Equal(t, chatgpt.LoginCancelled, waitLogin(t, f.svc.Official(), l.ID).State)
	_, ok := f.svc.Official().LoginStatus("unknown")
	assert.False(t, ok)
}

func TestRefresh_ManyCallersCauseOneRefreshRequest(t *testing.T) {
	f := newFixture(t)
	signInOfficial(t, f)
	f.auth.RefreshDelay = 100 * time.Millisecond
	f.advance(56 * time.Minute) // inside the five minutes before the hour is over

	var wg sync.WaitGroup
	tokens := make([]string, 20)
	for i := range tokens {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := f.svc.Official().Credentials(context.Background())
			assert.NoError(t, err)
			tokens[i] = c.AccessToken
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, countRefresh(f), "exactly one refresh request")
	for _, tok := range tokens {
		assert.Equal(t, tokens[0], tok, "every caller got the refreshed token")
	}
	_, err := f.svc.Official().Credentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, countRefresh(f), "a fresh token is not refreshed again")
}

func countRefresh(f *fixture) int { return f.auth.Count("/api/accounts/oauth/token") - 1 } // minus the code exchange

func TestRefresh_AnotherProcessThatRefreshedFirstIsNotRepeated(t *testing.T) {
	f := newFixture(t)
	signInOfficial(t, f)
	other := f.newService() // another mam of the same user: its own mutex, the same keys and lock file
	f.auth.RefreshDelay = 100 * time.Millisecond
	f.advance(58 * time.Minute)

	var wg sync.WaitGroup
	for _, svc := range []*chatgpt.Service{f.svc, other, f.svc, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Official().Credentials(context.Background())
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, countRefresh(f), "the lock file serializes the processes and the second one re-reads the tokens")
}

func TestRefresh_RotatesTheRefreshTokenAndKeepsWhatTheAnswerOmits(t *testing.T) {
	f := newFixture(t)
	signInOfficial(t, f)
	f.advance(59 * time.Minute)
	before, _ := tokenstore.New(f.keys, "official").Load()
	_, err := f.svc.Official().Credentials(context.Background())
	require.NoError(t, err)
	after, err := tokenstore.New(f.keys, "official").Load()
	require.NoError(t, err)
	assert.NotEqual(t, before.Refresh, after.Refresh, "the refresh token rotates")
	assert.NotEqual(t, before.Access, after.Access)
	assert.Equal(t, before.IDToken, after.IDToken, "the answer had no ID token: the old one is kept")
	assert.Equal(t, before.Meta.Scopes, after.Meta.Scopes)
	assert.Equal(t, "me@example.com", after.Meta.Email)
	assert.True(t, after.Meta.ExpiresAt.After(before.Meta.ExpiresAt))
}

func TestRefresh_ReloginAnswersMarkTheSignInAndNetworkFailuresDoNot(t *testing.T) {
	for _, code := range []string{"invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused"} {
		t.Run(code, func(t *testing.T) {
			f := newFixture(t)
			signInOfficial(t, f)
			f.auth.RefreshHandler = func(w http.ResponseWriter, _ url.Values) bool {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, `{"error":%q}`, code)
				return true
			}
			f.advance(2 * time.Hour) // the access token itself has expired too
			_, err := f.svc.Official().Credentials(context.Background())
			require.ErrorIs(t, err, chatgpt.ErrReloginRequired)

			st, err := f.svc.Official().Status()
			require.NoError(t, err)
			assert.True(t, st.SignedIn, "the tokens are kept")
			assert.True(t, st.NeedsRelogin)
			assert.Equal(t, code, st.ReloginReason)
			n := f.auth.Count("/api/accounts/oauth/token")
			_, err = f.svc.Official().Credentials(context.Background())
			require.ErrorIs(t, err, chatgpt.ErrReloginRequired)
			assert.Equal(t, n, f.auth.Count("/api/accounts/oauth/token"), "no more refresh attempts with a dead token")
			assert.Empty(t, f.leaks(err.Error()+f.logs.String()))

			signInOfficial(t, f) // signing in again clears the state
			st, _ = f.svc.Official().Status()
			assert.False(t, st.NeedsRelogin)
		})
	}

	t.Run("a 503 and a dropped connection never delete tokens", func(t *testing.T) {
		f := newFixture(t)
		signInOfficial(t, f)
		f.auth.RefreshHandler = func(w http.ResponseWriter, _ url.Values) bool { w.WriteHeader(503); return true }
		f.advance(2 * time.Hour)
		_, err := f.svc.Official().Credentials(context.Background())
		require.Error(t, err)
		require.NotErrorIs(t, err, chatgpt.ErrReloginRequired)
		st, _ := f.svc.Official().Status()
		assert.True(t, st.SignedIn)
		assert.False(t, st.NeedsRelogin)
	})

	t.Run("a refresh that fails while the token still lives uses the token", func(t *testing.T) {
		f := newFixture(t)
		signInOfficial(t, f)
		f.auth.RefreshHandler = func(w http.ResponseWriter, _ url.Values) bool { w.WriteHeader(502); return true }
		f.advance(57 * time.Minute)
		c, err := f.svc.Official().Credentials(context.Background())
		require.NoError(t, err)
		assert.Contains(t, c.AccessToken, "off-ACCESS-")
	})
}

func TestCredentials_NotSignedInAndMissingPlanScope(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Official().Credentials(context.Background())
	require.ErrorIs(t, err, chatgpt.ErrNotSignedIn)

	require.NoError(t, tokenstore.New(f.keys, "official").Save(tokenstore.Tokens{
		Access: "a", Refresh: "r", Meta: tokenstore.Meta{ClientID: chatgpttest.IssuedClient, ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"openid"}},
	}))
	_, err = f.svc.Official().Credentials(context.Background())
	require.ErrorIs(t, err, chatgpt.ErrPlanUsageNotGranted, "an identity without plan usage cannot call the API")
	st, _ := f.svc.Official().Status()
	assert.True(t, st.SignedIn)
	assert.False(t, st.PlanUsageGranted)
}
