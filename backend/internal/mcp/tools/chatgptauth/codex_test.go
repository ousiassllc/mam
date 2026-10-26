package chatgptauth_test

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/chatgptauth"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// newCodexFixture is the tools over a fake Codex app-server and an executable the user "installed".
func newCodexFixture(t *testing.T, extra ...func(*codex.Options)) (*fixture, *codextest.Fake) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home) // nothing may be read or written below the user's own home
	t.Setenv("USERPROFILE", home)
	var fake *codextest.Fake
	f := newFixture(t, true, func(o *chatgpt.Options) {
		fake = codextest.New(t, filepath.Join(o.DataDir, "codex-home"))
		reset, mins := int64(1_792_000_000), int64(300)
		fake.RateLimits = protocol.RateLimitsResult{RateLimits: protocol.RateLimitSnapshot{
			Primary: &protocol.RateLimitWindow{UsedPercent: 42, WindowDurationMins: &mins, ResetsAt: &reset},
		}}
		o.Codex = func(co *codex.Options) {
			co.Start = func(appserver.Launch) (appserver.Process, error) { return fake.Process(), nil }
			for _, e := range extra {
				e(co)
			}
		}
	})
	t.Cleanup(f.svc.Close)
	return f, fake
}

func useExecutable(t *testing.T, f *fixture) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "codex-app-server-test")
	require.NoError(t, os.WriteFile(exe, []byte("not a real executable"), 0o700))
	require.NoError(t, f.svc.Codex().SetCustomPath(exe))
}

func TestCodexStatus_NothingInstalledNothingSignedIn(t *testing.T) {
	f, _ := newCodexFixture(t)
	var out chatgptauth.StatusOutput
	f.env.CallOK("get_chatgpt_status", map[string]any{}, &out)
	assert.False(t, out.Codex.SignedIn)
	assert.Equal(t, "not_installed", out.Codex.Engine)
	assert.False(t, out.Codex.CanDownload, "the host is not Windows")
}

func TestCodexLogin_BrowserThenStatusLimitsAndLogoutWithoutAnyToken(t *testing.T) {
	f, fake := newCodexFixture(t)
	useExecutable(t, f)

	var login chatgptauth.LoginOutput
	f.env.CallOK("start_chatgpt_codex_login", map[string]any{"method": "browser"}, &login)
	assert.Equal(t, "codex", login.Account)
	assert.Equal(t, "pending", login.State)
	assert.NotEmpty(t, login.LoginID)
	assert.Contains(t, login.Note, "chatgpt-codex")

	fake.Notify(protocol.NotifLoginCompleted, map[string]any{"success": true, "loginId": login.LoginID})
	done := f.waitDone(login.LoginID)
	require.Equal(t, "succeeded", done.State, done.Message)

	var out chatgptauth.StatusOutput
	f.env.CallOK("get_chatgpt_status", map[string]any{}, &out)
	assert.True(t, out.Codex.SignedIn)
	assert.Equal(t, "user@example.invalid", out.Codex.Email)
	assert.Equal(t, "business", out.Codex.Plan)
	assert.Equal(t, "running", out.Codex.Engine)
	assert.Equal(t, "custom", out.Codex.EngineSource)

	var lo chatgptauth.LogoutOutput
	f.env.CallOK("logout_chatgpt", map[string]any{"account": "codex"}, &lo)
	assert.True(t, lo.WasSignedIn)
	assert.Len(t, fake.CallsOf(protocol.MethodLogout), 1)
	f.env.CallOK("get_chatgpt_status", map[string]any{}, &out)
	assert.False(t, out.Codex.SignedIn)

	for _, c := range []struct {
		name string
		args any
	}{
		{"get_chatgpt_status", map[string]any{}},
		{"get_chatgpt_login_status", map[string]any{"login_id": login.LoginID}},
		{"start_chatgpt_codex_login", map[string]any{"method": "device"}},
		{"logout_chatgpt", map[string]any{"account": "codex"}},
	} {
		seen := f.every(c.name, c.args)
		for _, marker := range []string{"-ACCESS-", "-REFRESH-", "secretsecret", "eyJ", "access_token", "refresh_token"} {
			assert.NotContains(t, seen, marker, "%s: a token-like value", c.name)
		}
	}
}

func TestCodexLogin_NeedsAnEngineAndAKnownMethod(t *testing.T) {
	f, _ := newCodexFixture(t)
	assert.Contains(t, f.env.CallError("start_chatgpt_codex_login", map[string]any{}), "install_chatgpt_codex_engine")
	useExecutable(t, f)
	assert.Contains(t, f.env.CallError("start_chatgpt_codex_login", map[string]any{"method": "pigeon"}), "browser か device")
	assert.Contains(t, f.env.CallError("logout_chatgpt", map[string]any{"account": "nobody"}), "codex")
}

func TestCodexInstall_IsRefusedWhereTheDownloadIsNotAvailable(t *testing.T) {
	f, _ := newCodexFixture(t)
	assert.Contains(t, f.env.CallError("install_chatgpt_codex_engine", map[string]any{}), "ダウンロードできません")
}

func TestCodexInstall_FailureIsReadThroughGetJob(t *testing.T) {
	f, _ := newCodexFixture(t, func(co *codex.Options) {
		c := catalog.Default()
		a := c.Assets["amd64"]
		a.URL = "http://127.0.0.1:1/never" // nothing listens there
		c.Assets = map[string]catalog.Asset{"amd64": a}
		co.Platform, co.Arch, co.Catalog = "windows", "amd64", c
		co.Layout = engine.Layout{Root: filepath.Join(t.TempDir(), "codex")}
		co.Fetch = fetch.Options{CheckURL: func(*url.URL) error { return nil }}
	})
	var job chatgptauth.CodexInstallOutput
	f.env.CallOK("install_chatgpt_codex_engine", map[string]any{}, &job)
	assert.Equal(t, mcpkit.JobQueued, job.State)
	f.svc.Codex().WaitInstall()

	var st mcpkit.JobStatus
	f.env.CallOK("get_job", map[string]any{"job_id": job.JobID}, &st)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.NotEmpty(t, st.Error)
	assert.Equal(t, chatgptauth.KindCodexInstall, st.Kind)
}
