package llmsettingstest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// managedStub is the llama.cpp of mam as the card sees it.
type managedStub struct{ info llmlocal.ManagedInfo }

func (m managedStub) Endpoint(context.Context) (string, string, error) { return "", "", nil }
func (m managedStub) Stop(context.Context) error                       { return nil }
func (m managedStub) Info(context.Context) llmlocal.ManagedInfo        { return m.info }

// linkScreen is the settings screen over the local LLM server of the service (a server of mam, when managed is not nil).
func linkScreen(t *testing.T, managed llmlocal.Managed) (*llmScreen, *llmlocal.Service) {
	t.Helper()
	keys := credentialtest.New()
	file := filepath.Join(t.TempDir(), "data", "llm.json")
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	st := settingsroutes.New(file, false, filepath.Join(t.TempDir(), "models.json"), false)
	st.LLMLocal = llmlocal.New(file, false, keys, nil)
	if managed != nil {
		st.LLMLocal.SetManaged(managed)
	}
	s := &llmScreen{t: t, file: file, engine: router.New(svc, router.Options{Settings: st})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		s.cookie = c
	}
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token = m[1]
	return s, st.LLMLocal
}

// cardOf is the card "ローカル LLM サーバー" of the page, from its <li> to the end of it.
func cardOf(t *testing.T, page string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<li data-testid="llm-local-card">.*?</li>`).FindString(page)
	require.NotEmpty(t, m, "the card of the local LLM server is on the page")
	return m
}

// The card has no field: it is a link to the model manager's page "ローカル LLM" with one line that says where the calls go now (#1104).
func TestLocalLLMCard_IsALinkWithTheStateAndNoField(t *testing.T) {
	s, _ := linkScreen(t, nil)

	page := s.page()
	card := cardOf(t, page)

	assert.NotContains(t, card, "<input")
	assert.NotContains(t, card, "<form")
	assert.Contains(t, card, "ローカル LLM サーバー")
	assert.Regexp(t, `<a [^>]*href="/model-manager/local-llm"[^>]*data-testid="llm-local-link"|<a [^>]*data-testid="llm-local-link"[^>]*href="/model-manager/local-llm"`, card)
	assert.Contains(t, card, "モデルマネージャーで設定する")
	assert.Regexp(t, `data-testid="llm-local-state"[^>]*>[^<]*今は 別のサーバー に呼び出します`, card)
	assert.Contains(t, page, `id="llm-assignment-panel"`, "the table of the assignment is still there")
	assert.NotContains(t, page, `name="base_url"`)
}

// The line is the state of the server in use: a llama.cpp of mam that is not installed is said, with what to do.
func TestLocalLLMCard_SaysWhereTheCallsGo(t *testing.T) {
	s, svc := linkScreen(t, managedStub{llmlocal.ManagedInfo{State: llmlocal.ManagedStopped, Model: "gemma-4-12b-it", ModelName: "Gemma"}})
	require.NoError(t, svc.SetUse(llmlocal.UseLlamaCpp))

	card := cardOf(t, s.page())

	assert.Contains(t, card, "今は この PC の llama.cpp に呼び出します")
	assert.NotContains(t, card, "未導入")

	s, svc = linkScreen(t, managedStub{llmlocal.ManagedInfo{State: llmlocal.ManagedNotInstalled, Reason: "llama.cpp が導入されていません"}})
	require.NoError(t, svc.SetUse(llmlocal.UseLlamaCpp))
	assert.Contains(t, cardOf(t, s.page()), "未導入")
}

// The save and the connection test of the card are gone: the routes are not registered (404) and nothing is written.
func TestLocalLLMCard_TheOldRoutesAreGone(t *testing.T) {
	s, _ := linkScreen(t, nil)

	put := s.requestTo(http.MethodPut, "/settings/llm/local", url.Values{"base_url": {"http://localhost:1234"}})
	test := s.requestTo(http.MethodPost, "/settings/llm/local/test", url.Values{"base_url": {"http://localhost:1234"}})

	assert.Equal(t, http.StatusNotFound, put.Code)
	assert.Equal(t, http.StatusNotFound, test.Code)
	_, err := filepath.Glob(s.file)
	require.NoError(t, err)
	assert.NoFileExists(t, s.file)
}
