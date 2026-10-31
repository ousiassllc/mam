package chatgptauth_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/chatgpttest"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/chatgptauth"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
)

type fixture struct {
	env  *mcptest.Env
	auth *chatgpttest.Server
	svc  *chatgpt.Service
}

func newFixture(t *testing.T, withService bool, tweak ...func(*chatgpt.Options)) *fixture {
	t.Helper()
	f := &fixture{auth: chatgpttest.NewServer(t)}
	f.env = mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) {
		env.Keys.LimitBytes(2560) // the Windows Credential Manager limit
		if withService {
			opts := chatgpt.Options{
				Keys: env.Keys, DataDir: t.TempDir(), LockPath: t.TempDir() + "/.auth-refresh.lock", Issuer: f.auth.URL,
				CallbackPorts: []int{0}, // not the real 1455 and 1457: every process of the host shares them
				Sleep:         func(context.Context, time.Duration) error { return nil },
				Open:          func(u string) error { f.auth.Approve(u); return nil },
			}
			for _, tw := range tweak {
				tw(&opts)
			}
			f.svc = chatgpt.New(opts)
			r.Settings.ChatGPT = f.svc
		}
		chatgptauth.Register(r, env.Services)
		jobs.Register(r, env.Services) // get_job reads the install job
	})
	return f
}

// every is all the text a client can see of a call: the content blocks and the structured output.
func (f *fixture) every(name string, args any) string {
	f.env.T.Helper()
	raw, err := json.Marshal(f.env.Call(name, args))
	require.NoError(f.env.T, err)
	return string(raw)
}

// waitDone polls get_chatgpt_login_status until the login leaves pending.
func (f *fixture) waitDone(id string) chatgptauth.LoginOutput {
	f.env.T.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var out chatgptauth.LoginOutput
		f.env.CallOK("get_chatgpt_login_status", map[string]any{"login_id": id}, &out)
		if out.State != "pending" {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.FailNow(f.env.T, "login stayed pending")
	return chatgptauth.LoginOutput{}
}

// waitCodexAccount polls get_chatgpt_status until the Codex account is signed in (the account is read in the
// background after the login completed) and returns that status.
func (f *fixture) waitCodexAccount() chatgptauth.StatusOutput {
	f.env.T.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var out chatgptauth.StatusOutput
		f.env.CallOK("get_chatgpt_status", map[string]any{}, &out)
		if out.Codex.SignedIn {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.FailNow(f.env.T, "the Codex account was never read")
	return chatgptauth.StatusOutput{}
}

func TestStatus_NothingSignedInImageOffAndTheRisksAreShown(t *testing.T) {
	f := newFixture(t, true)
	var out chatgptauth.StatusOutput
	f.env.CallOK("get_chatgpt_status", map[string]any{}, &out)
	assert.False(t, out.Official.SignedIn)
	assert.False(t, out.Image.SignedIn)
	assert.False(t, out.ImageGenerationEnabled, "OFF by default")
	assert.Equal(t, "https://chatgpt.com/settings/usage", out.UsageURL)
	assert.Len(t, out.ImageRisks, 3)
}

func TestOfficialLogin_ThroughMCPAndNoToolEverShowsAToken(t *testing.T) {
	f := newFixture(t, true)
	var login chatgptauth.LoginOutput
	f.env.CallOK("start_chatgpt_login", map[string]any{}, &login)
	assert.Equal(t, "official", login.Account)
	assert.Equal(t, "browser", login.Method)
	assert.Contains(t, login.AuthURL, "/api/accounts/authorize?") // the authorization URL to open on the machine mam runs on
	assert.Contains(t, login.AuthURL, "127.0.0.1")
	assert.Contains(t, login.Note, "リモート")
	assert.NotEmpty(t, login.LoginID)

	done := f.waitDone(login.LoginID)
	require.Equal(t, "succeeded", done.State, done.Message)

	var st chatgptauth.StatusOutput
	f.env.CallOK("get_chatgpt_status", map[string]any{}, &st)
	assert.True(t, st.Official.SignedIn)
	assert.Equal(t, "me@example.com", st.Official.Email)
	assert.Equal(t, "plus", st.Official.Plan)
	require.NotNil(t, st.Official.PlanUsageGranted)
	assert.True(t, *st.Official.PlanUsageGranted)
	assert.NotEmpty(t, st.Official.ExpiresAt)
	assert.False(t, st.Official.NeedsRelogin)

	// Every result of every tool, as the client sees it, holds no token of what the key store holds.
	secrets := storedSecrets(t, f)
	require.NotEmpty(t, secrets)
	for _, c := range []struct {
		name string
		args any
	}{
		{"get_chatgpt_status", map[string]any{}},
		{"get_chatgpt_login_status", map[string]any{"login_id": login.LoginID}},
		{"get_chatgpt_login_status", map[string]any{"login_id": "no-such-login"}},
		{"start_chatgpt_login", map[string]any{}},
		{"set_chatgpt_image_generation", map[string]any{"enabled": true}},
		{"set_chatgpt_image_generation", map[string]any{"enabled": true, "acknowledge_risks": true}},
		{"start_chatgpt_image_device_login", map[string]any{}},
		{"logout_chatgpt", map[string]any{"account": "official"}},
		{"logout_chatgpt", map[string]any{"account": "official"}},
		{"logout_chatgpt", map[string]any{"account": "nobody"}},
		{"logout_chatgpt", map[string]any{"account": "image"}},
	} {
		seen := f.every(c.name, c.args)
		for _, s := range secrets {
			assert.NotContains(t, seen, s, c.name)
		}
		for _, marker := range []string{"-ACCESS-", "-REFRESH-", "secretsecret", "eyJ"} {
			// the random values of an authorization attempt hold "eyJ" now and then
			assert.NotContains(t, chatgpttest.WithoutAttemptValues(seen), marker, "%s: a token-like value", c.name)
		}
	}
}

// storedSecrets are the values of the sign-in entries, read from the key store (the way only the adapters read them).
func storedSecrets(t *testing.T, f *fixture) []string {
	t.Helper()
	creds, err := f.svc.Official().Credentials(t.Context())
	require.NoError(t, err)
	out := []string{creds.AccessToken}
	for _, field := range []string{"refresh", "id_token"} {
		v, err := f.env.Keys.Get("chatgpt/official/" + field)
		require.NoError(t, err)
		out = append(out, v)
	}
	return out
}

func TestLogout_RevokesAndClearsAndReportsAnUnconfirmedRevoke(t *testing.T) {
	f := newFixture(t, true)
	var login chatgptauth.LoginOutput
	f.env.CallOK("start_chatgpt_login", map[string]any{}, &login)
	require.Equal(t, "succeeded", f.waitDone(login.LoginID).State)

	var out chatgptauth.LogoutOutput
	f.env.CallOK("logout_chatgpt", map[string]any{"account": "official"}, &out)
	assert.True(t, out.WasSignedIn)
	assert.True(t, out.RevokeConfirmed)
	assert.Equal(t, 1, f.auth.Count("/api/accounts/oauth/revoke"))
	ok, _ := f.env.Keys.Has("chatgpt/official/refresh")
	assert.False(t, ok)

	f.env.CallOK("logout_chatgpt", map[string]any{"account": "official"}, &out)
	assert.False(t, out.WasSignedIn, "a second logout is harmless")

	f.env.CallOK("start_chatgpt_login", map[string]any{}, &login)
	require.Equal(t, "succeeded", f.waitDone(login.LoginID).State)
	f.auth.RevokeStatus = 503
	f.env.CallOK("logout_chatgpt", map[string]any{"account": "official"}, &out)
	assert.False(t, out.RevokeConfirmed)
	assert.Contains(t, out.Message, "失効を確認できませんでした")
	ok, _ = f.env.Keys.Has("chatgpt/official/refresh")
	assert.False(t, ok, "the local tokens are gone anyway")

	assert.Contains(t, f.env.CallError("logout_chatgpt", map[string]any{"account": "nobody"}), "validation")
}

func TestImageGeneration_RisksAreRequiredToTurnItOnAndTheDeviceLoginNeedsItOn(t *testing.T) {
	f := newFixture(t, true)
	assert.Contains(t, f.env.CallError("start_chatgpt_image_device_login", map[string]any{}), "conflict")
	assert.Zero(t, f.auth.Total(), "OFF: no request")

	msg := f.env.CallError("set_chatgpt_image_generation", map[string]any{"enabled": true})
	assert.Contains(t, msg, "validation")
	assert.Contains(t, msg, "非公式")
	var ov chatgptauth.StatusOutput
	f.env.CallOK("get_chatgpt_status", map[string]any{}, &ov)
	assert.False(t, ov.ImageGenerationEnabled)

	var sw chatgptauth.ImageSwitchOutput
	f.env.CallOK("set_chatgpt_image_generation", map[string]any{"enabled": true, "acknowledge_risks": true}, &sw)
	assert.True(t, sw.ImageGenerationEnabled)
	assert.Len(t, sw.ImageRisks, 3, "the response carries the risks")

	f.auth.PendingPolls = 1
	var login chatgptauth.LoginOutput
	f.env.CallOK("start_chatgpt_image_device_login", map[string]any{}, &login)
	assert.Equal(t, "image", login.Account)
	assert.Equal(t, "device", login.Method)
	assert.Equal(t, f.auth.URL+"/codex/device", login.VerificationURL)
	assert.Equal(t, "ABCD-1234", login.UserCode)
	assert.Equal(t, 1, login.IntervalSeconds)
	assert.NotEmpty(t, login.ExpiresAt)
	done := f.waitDone(login.LoginID)
	require.Equal(t, "succeeded", done.State, done.Message)

	var st chatgptauth.StatusOutput
	f.env.CallOK("get_chatgpt_status", map[string]any{}, &st)
	assert.True(t, st.Image.SignedIn)
	assert.True(t, st.ImageGenerationEnabled)
	assert.Nil(t, st.Image.PlanUsageGranted, "plan usage scope is an official-login notion")

	f.env.CallOK("set_chatgpt_image_generation", map[string]any{"enabled": false}, &sw)
	assert.False(t, sw.ImageGenerationEnabled, "OFF needs no confirmation")
}

func TestTools_AnswerUnavailableWithoutTheService(t *testing.T) {
	f := newFixture(t, false)
	for _, name := range []string{"get_chatgpt_status", "start_chatgpt_login", "start_chatgpt_image_device_login"} {
		assert.Contains(t, f.env.CallError(name, map[string]any{}), "unavailable", name)
	}
	assert.Contains(t, f.env.CallError("logout_chatgpt", map[string]any{"account": "official"}), "unavailable")
	assert.Contains(t, f.env.CallError("get_chatgpt_login_status", map[string]any{"login_id": "x"}), "unavailable")
	assert.Contains(t, f.env.CallError("set_chatgpt_image_generation", map[string]any{"enabled": false}), "unavailable")
}

func TestToolDescriptions_CarryTheRisksAndTheRemoteGuidance(t *testing.T) {
	f := newFixture(t, true)
	tools, err := f.env.Client.ListTools(t.Context(), nil)
	require.NoError(t, err)
	desc := map[string]string{}
	for _, tool := range tools.Tools {
		desc[tool.Name] = tool.Description
	}
	for _, want := range []string{"非公式", "サポート外", "利用上限", "acknowledge_risks"} {
		assert.Contains(t, desc["set_chatgpt_image_generation"], want)
	}
	assert.Contains(t, desc["start_chatgpt_login"], "127.0.0.1")
	assert.True(t, strings.Contains(desc["start_chatgpt_login"], "リモート"))
}
