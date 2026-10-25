package chatgpt_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/oauth"
)

func callbackWithError(f *fixture, code string) {
	f.browser = func(authURL string) {
		u, _ := url.Parse(authURL)
		q := u.Query()
		cb, _ := url.Parse(q.Get("redirect_uri"))
		go func() {
			res, err := http.Get(cb.String() + "?error=" + code + "&state=" + q.Get("state")) //nolint:gosec,noctx // test
			if err == nil {
				_ = res.Body.Close()
			}
		}()
	}
}

// An organization account that the policy refuses gets the reason and the way out: the Codex login.
func TestOfficialLogin_PolicyDeniedExplainsAndPointsToTheCodexLogin(t *testing.T) {
	f := newFixture(t)
	callbackWithError(f, oauth.PolicyDeniedCode)
	l, err := f.svc.Official().StartBrowserLogin()
	require.NoError(t, err)
	got := waitLogin(t, f.svc.Official(), l.ID)
	assert.Equal(t, chatgpt.LoginFailed, got.State)
	assert.Equal(t, chatgpt.PolicyDeniedMessage, got.Message)
	assert.Contains(t, got.Message, "Codex 経由でログインしてください")
	assert.True(t, got.UseCodex, "the screen shows the button of the Codex login")
	st, _ := f.svc.Official().Status()
	assert.False(t, st.SignedIn)
}

func TestOfficialLogin_OtherServerErrorsStayGenericAndDoNotPointToCodex(t *testing.T) {
	f := newFixture(t)
	callbackWithError(f, "server_error")
	l, err := f.svc.Official().StartBrowserLogin()
	require.NoError(t, err)
	got := waitLogin(t, f.svc.Official(), l.ID)
	assert.Equal(t, chatgpt.LoginFailed, got.State)
	assert.Contains(t, got.Message, "ログインを完了できませんでした")
	assert.Contains(t, got.Message, "server_error")
	assert.False(t, got.UseCodex)
}

func TestIsWorkspacePlan(t *testing.T) {
	for plan, want := range map[string]bool{
		"business": true, "enterprise": true, "edu": true, "edu_plus": true, "team": true, "ent26": true, "Enterprise": true,
		"self_serve_business_prolite": true, "self_serve_business_usage_based": true, "enterprise_cbp_usage_based": true,
		"plus": false, "pro": false, "go": false, "free": false, "prolite": false, "promax": false, "unknown": false, "": false,
	} {
		assert.Equal(t, want, chatgpt.IsWorkspacePlan(plan), plan)
	}
}

func TestOfficialStatus_ShowsWhetherThePlanIsAWorkspaceOne(t *testing.T) {
	f := newFixture(t)
	l, err := f.svc.Official().StartBrowserLogin()
	require.NoError(t, err)
	require.Equal(t, chatgpt.LoginSucceeded, waitLogin(t, f.svc.Official(), l.ID).State)
	st, err := f.svc.Official().Status()
	require.NoError(t, err)
	assert.Equal(t, "plus", st.Plan)
	assert.False(t, st.WorkspacePlan)
}

func TestServiceHasTheCodexLoginAndClosesIt(t *testing.T) {
	f := newFixture(t)
	require.NotNil(t, f.svc.Codex())
	assert.Equal(t, "not_installed", string(f.svc.Codex().Status().Engine))
	f.svc.Close()
}
