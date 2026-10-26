package chatgpt_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/chatgpttest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/tokenstore"
)

func TestImageGeneration_IsOffByDefaultAndOffMeansNoCallAndNoLogin(t *testing.T) {
	f := newFixture(t)
	assert.False(t, f.svc.ImageEnabled())

	_, err := f.svc.ImageCredentials(context.Background())
	require.ErrorIs(t, err, chatgpt.ErrImageDisabled)
	_, err = f.svc.StartImageBrowserLogin()
	require.ErrorIs(t, err, chatgpt.ErrImageDisabled)
	_, err = f.svc.StartImageDeviceLogin(context.Background())
	require.ErrorIs(t, err, chatgpt.ErrImageDisabled)
	assert.Zero(t, f.auth.Total(), "OFF: no request at all to the authorization server")
	assert.Empty(t, f.opened, "OFF: no browser")

	// A sign-in that exists does not make the path callable while it is off, and even a due refresh is not attempted.
	require.NoError(t, tokenstore.New(f.keys, "image").Save(tokenstore.Tokens{
		Access: "a", Refresh: "r", Meta: tokenstore.Meta{ExpiresAt: time.Now().Add(-time.Hour)},
	}))
	_, err = f.svc.ImageCredentials(context.Background())
	require.ErrorIs(t, err, chatgpt.ErrImageDisabled)
	assert.Zero(t, f.auth.Total())
}

func TestImageGeneration_TurningItOnNeedsTheConfirmationAndSurvivesARestart(t *testing.T) {
	f := newFixture(t)
	require.ErrorIs(t, f.svc.SetImageEnabled(true, false), chatgpt.ErrRisksNotAcknowledged)
	assert.False(t, f.svc.ImageEnabled(), "refused: still off")

	require.NoError(t, f.svc.SetImageEnabled(true, true))
	assert.True(t, f.svc.ImageEnabled())
	assert.True(t, f.newService().ImageEnabled(), "a new process reads the file")
	raw, err := os.ReadFile(filepath.Join(f.dataDir, chatgpt.SettingsFile))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "token")

	require.NoError(t, f.svc.SetImageEnabled(false, false), "off needs no confirmation")
	assert.False(t, f.svc.ImageEnabled())
	require.ErrorIs(t, f.svc.SetImageEnabled(true, false), chatgpt.ErrRisksNotAcknowledged, "every ON asks again")

	require.NoError(t, os.WriteFile(filepath.Join(f.dataDir, chatgpt.SettingsFile), []byte("{broken"), 0o600))
	assert.False(t, f.svc.ImageEnabled(), "a damaged file means off")

	assert.Len(t, chatgpt.ImageRisks, 3)
	for _, want := range []string{"非公式", "サポート外", "利用上限"} {
		assert.Contains(t, chatgpt.ImageRisksSummary(), want)
	}
}

func TestImageDeviceLogin_ShowsTheCodeAndSignsIn(t *testing.T) {
	f := newFixture(t)
	f.auth.PendingPolls = 2
	require.NoError(t, f.svc.SetImageEnabled(true, true))

	l, err := f.svc.StartImageDeviceLogin(context.Background())
	require.NoError(t, err)
	assert.Equal(t, chatgpt.MethodDevice, l.Method)
	assert.Equal(t, f.auth.URL+"/codex/device", l.VerificationURL)
	assert.Equal(t, "ABCD-1234", l.UserCode)
	assert.Equal(t, time.Second, l.Interval)
	assert.True(t, l.ExpiresAt.After(time.Now()))

	got := waitLogin(t, f.svc.Image(), l.ID)
	require.Equal(t, chatgpt.LoginSucceeded, got.State, got.Message)
	st, err := f.svc.Image().Status()
	require.NoError(t, err)
	assert.True(t, st.SignedIn)
	assert.Equal(t, "codex@example.com", st.Email)
	creds, err := f.svc.ImageCredentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "acct-1", creds.AccountID, "the account id header comes from the ID token claim")
	assert.Contains(t, creds.AccessToken, "cdx-ACCESS-")
	assert.Equal(t, 3, f.auth.Count("/api/accounts/deviceauth/token"))

	ex := f.auth.Count("/oauth/token")
	assert.Equal(t, 1, ex, "one code exchange")
	assert.Empty(t, f.leaks(f.logs.String()))
}

func TestImageBrowserLogin_UsesTheCodexClientOnItsOwnTokens(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.svc.SetImageEnabled(true, true))
	l, err := f.svc.StartImageBrowserLogin()
	require.NoError(t, err)
	assert.Contains(t, l.AuthURL, f.auth.URL+"/oauth/authorize")
	assert.Contains(t, l.AuthURL, "client_id="+chatgpttest.CodexClient)
	got := waitLogin(t, f.svc.Image(), l.ID)
	require.Equal(t, chatgpt.LoginSucceeded, got.State, got.Message)

	off, _ := f.svc.Official().Status()
	assert.False(t, off.SignedIn, "the two login systems are separate")
	ok, _ := f.keys.Has("chatgpt/image/refresh")
	assert.True(t, ok)
	ok, _ = f.keys.Has("chatgpt/official/refresh")
	assert.False(t, ok)
}

func TestOfficialHasNoDeviceLogin(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Official().StartDeviceLogin(context.Background())
	require.ErrorIs(t, err, chatgpt.ErrNoDeviceLogin)
	assert.Zero(t, f.auth.Total())
}

func TestLogout_RevokesThenDeletesAndDeletesEvenWhenRevocationFails(t *testing.T) {
	f := newFixture(t)
	signInOfficial(t, f)

	res, err := f.svc.Official().Logout(context.Background())
	require.NoError(t, err)
	assert.True(t, res.WasSignedIn)
	assert.True(t, res.RevokeConfirmed)
	assert.Equal(t, 1, f.auth.Count("/api/accounts/oauth/revoke"))
	st, _ := f.svc.Official().Status()
	assert.False(t, st.SignedIn)
	reg, _ := tokenstore.New(f.keys, "official").Registration()
	assert.Equal(t, chatgpttest.IssuedClient, reg.ClientID, "the registration survives a sign-out")

	signInOfficial(t, f)
	f.auth.RevokeStatus = http.StatusServiceUnavailable
	res, err = f.svc.Official().Logout(context.Background())
	require.NoError(t, err)
	assert.False(t, res.RevokeConfirmed, "unconfirmed, and the user is told")
	assert.NotEmpty(t, res.RevokeProblem)
	assert.Empty(t, f.leaks(res.RevokeProblem))
	st, _ = f.svc.Official().Status()
	assert.False(t, st.SignedIn, "the local tokens are gone anyway")
	for _, k := range []string{"access", "access/0", "refresh", "id_token", "meta"} {
		ok, _ := f.keys.Has("chatgpt/official/" + k)
		assert.False(t, ok, k)
	}
	assert.GreaterOrEqual(t, f.auth.Count("/api/accounts/oauth/revoke"), 1+3, "a 5xx is retried")

	res, err = f.svc.Official().Logout(context.Background())
	require.NoError(t, err)
	assert.False(t, res.WasSignedIn, "logging out twice is harmless")
}

func TestLogout_CodexRevokesWithJSON(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.svc.SetImageEnabled(true, true))
	l, err := f.svc.StartImageBrowserLogin()
	require.NoError(t, err)
	require.Equal(t, chatgpt.LoginSucceeded, waitLogin(t, f.svc.Image(), l.ID).State)
	res, err := f.svc.Image().Logout(context.Background())
	require.NoError(t, err)
	assert.True(t, res.RevokeConfirmed)
	assert.Equal(t, 1, f.auth.Count("/oauth/revoke"))
}

func TestModels_ListsTheVisibleModelsWithTheSameAccessToken(t *testing.T) {
	f := newFixture(t)
	var auth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
			{"slug": "m-a", "display_name": "Model A", "visibility": "list"},
			{"slug": "m-hidden", "display_name": "Hidden", "visibility": "hide"},
			{"slug": "m-b", "display_name": "Model B", "visibility": "list"},
		}})
	}))
	defer api.Close()
	svc := chatgpt.New(chatgpt.Options{Keys: f.keys, DataDir: f.dataDir, LockPath: f.lock, Issuer: f.auth.URL, APIBase: api.URL})
	_, err := svc.Models(context.Background())
	require.ErrorIs(t, err, chatgpt.ErrNotSignedIn)

	signInOfficial(t, f)
	models, err := svc.Models(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []chatgpt.Model{{Slug: "m-a", DisplayName: "Model A"}, {Slug: "m-b", DisplayName: "Model B"}}, models)
	assert.Contains(t, auth, "Bearer off-ACCESS-")
}
