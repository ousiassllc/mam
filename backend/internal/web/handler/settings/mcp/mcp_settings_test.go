package mcp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/mcp/bind"
	mcptokenrepo "github.com/ousiassllc/mam/backend/internal/repository/mcptoken"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/mcpsettings"
	"github.com/ousiassllc/mam/backend/internal/service/mcptoken"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// fakeListener is the MCP listener as the settings screen sees it.
type fakeListener struct {
	addr, saved, problem string
	failWith             error
}

func (f *fakeListener) Status() mcpsettings.Status {
	return mcpsettings.Status{Addr: f.addr, Saved: f.saved, Problem: f.problem, Candidates: []string{"127.0.0.1", "100.101.102.103"}}
}

func (f *fakeListener) Rebind(host string) error {
	if f.failWith != nil {
		return f.failWith
	}
	if host == "0.0.0.0" {
		return &bind.RejectedError{Message: "0.0.0.0 ではすべてのネットワークに公開されるため使えません"}
	}
	f.saved, f.addr = host, host+":8788"
	return nil
}

type mcpScreen struct {
	t        *testing.T
	engine   http.Handler
	cookie   *http.Cookie
	token    string
	page     string
	tokens   *mcptoken.Service
	listener *fakeListener
}

func newMCPScreen(t *testing.T, withMCP bool) *mcpScreen {
	t.Helper()
	db := testdb.Open(t)
	svc := servicetest.New(t, service.Deps{DB: db, Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "models.json"), false)
	s := &mcpScreen{t: t, tokens: mcptoken.New(mcptokenrepo.New(db)), listener: &fakeListener{addr: "127.0.0.1:8788", saved: "127.0.0.1"}}
	if withMCP {
		settings.MCP = &mcpsettings.Service{Tokens: s.tokens, Listener: s.listener}
	}
	s.engine = router.New(svc, router.Options{Settings: settings})
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			s.cookie = c
		}
	}
	require.NotNil(t, s.cookie)
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token, s.page = m[1], rec.Body.String()
	if withMCP { // the MCP category has its own page; without the server the overview is all there is
		page := s.send(httptest.NewRequest(http.MethodGet, paths.MCPSettings(), nil))
		require.Equal(t, http.StatusOK, page.Code)
		s.page = page.Body.String()
	}
	return s
}

func (s *mcpScreen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

func (s *mcpScreen) act(method, target string, form url.Values, withToken bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	if withToken {
		req.Header.Set(middleware.CSRFHeaderName, s.token)
	}
	return s.send(req)
}

func (s *mcpScreen) get() string {
	req := httptest.NewRequest(http.MethodGet, paths.MCPSettings(), nil)
	req.Header.Set("HX-Request", "true")
	return s.send(req).Body.String()
}

func issuedValue(t *testing.T, html string) string {
	t.Helper()
	m := regexp.MustCompile(`value="(mam_mcp_[A-Za-z0-9_-]+)"[^>]*data-testid="mcp-issued-value"`).FindStringSubmatch(html)
	require.Len(t, m, 2, html)
	return m[1]
}

func TestMCPSettings_WithoutTheMCPServerThereIsNoSectionAndNoRoute(t *testing.T) {
	s := newMCPScreen(t, false)
	assert.NotContains(t, s.page, "MCP サーバー")
	assert.NotContains(t, s.page, paths.MCPTokens())
	rec := s.act(http.MethodPost, paths.MCPTokens(), url.Values{"name": {"x"}}, true)
	assert.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, rec.Code)
}

func TestMCPSettings_IssueShowsTheValueOnceAndTheListNeverAgain(t *testing.T) {
	s := newMCPScreen(t, true)
	assert.Contains(t, s.page, "MCP サーバー")
	assert.Contains(t, s.page, "http://127.0.0.1:8788/mcp")
	assert.Contains(t, s.page, "発行済みのトークンはありません")

	rec := s.act(http.MethodPost, paths.MCPTokens(), url.Values{"name": {"Grok Bot"}}, true)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	value := issuedValue(t, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "今しか表示されません")
	assert.Equal(t, 1, strings.Count(rec.Body.String(), value), "the value appears once in the answer")
	assert.Contains(t, rec.Body.String(), "Grok Bot")
	assert.Contains(t, rec.Body.String(), "未使用")
	assert.Contains(t, rec.Body.String(), `data-copy-source="mcp-issued-value"`, "the copy button reads the field of the value")
	assert.Empty(t, rec.Header().Get("HX-Trigger-After-Swap"), "the modal stays open to show the value")
	assert.Contains(t, rec.Body.String(), `hx-swap-oob="beforeend:#mcp-token-list"`, "the row joins the table, without the value")
	assert.Contains(t, rec.Body.String(), `<span id="mcp-tokens-empty" hx-swap-oob="delete">`)

	// The value works as a bearer token, and no later view of the section shows it.
	tok, err := s.tokens.Verify(context.Background(), value)
	require.NoError(t, err)
	later := s.get()
	assert.NotContains(t, later, value)
	assert.NotContains(t, later, strings.TrimPrefix(value, "mam_mcp_"), "not even without the prefix")
	assert.Contains(t, later, "mam_mcp_"+tok.Prefix+"…", "only the start that tells tokens apart")
	assert.Contains(t, later, "有効")
	assert.NotContains(t, later, "mcp-issued")
}

func TestMCPSettings_ActionsNeedTheCSRFToken(t *testing.T) {
	s := newMCPScreen(t, true)
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"issue":  s.act(http.MethodPost, paths.MCPTokens(), url.Values{"name": {"x"}}, false),
		"bind":   s.act(http.MethodPut, paths.MCPBind(), url.Values{"bind_address": {"127.0.0.1"}}, false),
		"revoke": s.act(http.MethodDelete, paths.MCPToken("any"), nil, false),
	} {
		assert.Equal(t, http.StatusForbidden, rec.Code, name)
	}
	list, err := s.tokens.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, list, "a request without the token issued nothing")
}

func TestMCPSettings_ABadNameIsRefusedWithTheReasonAndWhatWasTyped(t *testing.T) {
	s := newMCPScreen(t, true)
	rec := s.act(http.MethodPost, paths.MCPTokens(), url.Values{"name": {"   "}}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "トークンの名前を入力してください")
	assert.NotContains(t, rec.Body.String(), "mcp-issued-value")

	rec = s.act(http.MethodPost, paths.MCPTokens(), url.Values{"name": {strings.Repeat("a", 65)}}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), strings.Repeat("a", 65), "the form keeps what was typed")
}

func TestMCPSettings_RevokeEndsTheTokenAndKeepsItListed(t *testing.T) {
	s := newMCPScreen(t, true)
	rec := s.act(http.MethodPost, paths.MCPTokens(), url.Values{"name": {"old"}}, true)
	value := issuedValue(t, rec.Body.String())
	list, err := s.tokens.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Contains(t, s.get(), `hx-delete="`+paths.MCPToken(list[0].ID)+`"`)

	rec = s.act(http.MethodDelete, paths.MCPToken(list[0].ID), nil, true)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "失効済み")
	assert.NotContains(t, rec.Body.String(), "hx-delete=", "a revoked token has no revoke button")
	_, err = s.tokens.Verify(context.Background(), value)
	assert.ErrorIs(t, err, mcptoken.ErrInvalidToken)

	rec = s.act(http.MethodDelete, paths.MCPToken("missing"), nil, true)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "見つかりません")
}

func TestMCPSettings_BindAddressSwitchRefusalsAndFailures(t *testing.T) {
	s := newMCPScreen(t, true)

	rec := s.act(http.MethodPut, paths.MCPBind(), url.Values{"bind_address": {" 100.101.102.103 "}}, true)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "100.101.102.103 に切り替えました")
	assert.Contains(t, rec.Body.String(), "http://100.101.102.103:8788/mcp")
	assert.Equal(t, "100.101.102.103", s.listener.saved)

	rec = s.act(http.MethodPut, paths.MCPBind(), url.Values{"bind_address": {"0.0.0.0"}}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "すべてのネットワークに公開されるため使えません")
	assert.Contains(t, rec.Body.String(), `value="0.0.0.0"`, "the form keeps what was typed")
	assert.Equal(t, "100.101.102.103", s.listener.saved, "a refused address changes nothing")

	s.listener.failWith = errors.New("listen tcp 100.101.102.103:8788: bind: address already in use")
	rec = s.act(http.MethodPut, paths.MCPBind(), url.Values{"bind_address": {"127.0.0.1"}}, true)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "元の待ち受け先のままです")
	assert.NotContains(t, rec.Body.String(), "address already in use", "the cause is for the log, not the page")
}

func TestMCPSettings_TheSectionSaysWhyItIsNotOnTheSavedAddress(t *testing.T) {
	s := newMCPScreen(t, true)
	s.listener.addr, s.listener.problem = "", "保存済みの待ち受け先 100.101.102.103 を使えないため、127.0.0.1 で待ち受けています"

	page := s.get()
	assert.Regexp(t, `role="alert"[^>]*data-testid="mcp-problem">保存済みの待ち受け先 100\.101\.102\.103`, page)
	assert.Contains(t, page, "待ち受けていません")
}
