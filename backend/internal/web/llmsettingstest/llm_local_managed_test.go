package llmsettingstest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// managedStub is the llama.cpp of mam as the card sees it.
type managedStub struct{ info llmlocal.ManagedInfo }

func (m managedStub) Endpoint(context.Context) (string, string, error) { return "", "", nil }
func (m managedStub) Stop(context.Context) error                       { return nil }
func (m managedStub) Info(context.Context) llmlocal.ManagedInfo        { return m.info }

func managedScreen(t *testing.T, pinned bool, managed llmlocal.Managed) *llmScreen {
	t.Helper()
	keys := credentialtest.New()
	file := filepath.Join(t.TempDir(), "data", "llm.json")
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	st := settingsroutes.New(file, pinned, filepath.Join(t.TempDir(), "models.json"), false)
	st.LLMLocal = llmlocal.New(file, pinned, keys, nil)
	if managed != nil {
		st.LLMLocal.SetManaged(managed)
	}
	st.LLMModels = llmmodels.New(llmmodels.Options{Keys: keys, LocalBaseURL: st.LLMLocal.BaseURL, LocalManaged: st.LLMLocal.ManagedModels})
	s := &llmScreen{t: t, file: file, engine: router.New(svc, router.Options{Settings: st})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		s.cookie = c
	}
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token = m[1]
	return s
}

var gemmaStopped = managedStub{llmlocal.ManagedInfo{State: llmlocal.ManagedStopped, Model: "gemma-4-12b-it", ModelName: "Gemma 4 12B-it（純正・Q4_K_M）"}}

// The card has the choice "この PC の llama.cpp を使う" with where the server stands, and a link to the model manager where it is installed and its model chosen.
func TestManagedCard_OffersThisPCsLlamaCppWithItsStateAndALinkToTheModelManager(t *testing.T) {
	s := managedScreen(t, false, gemmaStopped)

	page := s.page()

	assert.Contains(t, page, `data-testid="llm-local-use-managed"`)
	assert.Contains(t, page, "この PC の llama.cpp を使う")
	assert.NotContains(t, inputTag(t, page, "use_managed"), " checked", "off until the user chooses it: the address of the settings is in force")
	assert.Contains(t, page, "導入済みです（モデル: Gemma 4 12B-it（純正・Q4_K_M））")
	assert.Contains(t, page, `href="`+paths.ModelManager()+`"`)
	assert.Contains(t, page, `name="base_url"`, "the address stays for another server")
}

func TestManagedCard_SaveChoosesItAndTheChoiceIsKeptAndShownOn(t *testing.T) {
	s := managedScreen(t, false, gemmaStopped)

	rec := s.requestTo(http.MethodPut, paths.LLMLocal(), url.Values{"use_managed_present": {"1"}, "use_managed": {"1"}, "unload_present": {"1"}})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, inputTag(t, rec.Body.String(), "use_managed"), " checked")
	raw, err := os.ReadFile(s.file)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"use_managed_runtime": true`)
	cfg, err := llm.LoadConfig(s.file)
	require.NoError(t, err)
	assert.True(t, cfg.Providers[llm.ProviderLocal].UseManagedRuntime)

	off := s.requestTo(http.MethodPut, paths.LLMLocal(), url.Values{"use_managed_present": {"1"}, "unload_present": {"1"}})
	require.Equal(t, http.StatusOK, off.Code)
	assert.NotContains(t, inputTag(t, off.Body.String(), "use_managed"), " checked")
	raw, _ = os.ReadFile(s.file)
	assert.NotContains(t, string(raw), "use_managed_runtime", "the default is not written")
}

// A PC that cannot run it, or that has not installed it, says why on the card (with what to do), in the amber line.
func TestManagedCard_ShowsWhyItCannotBeUsed(t *testing.T) {
	for name, tc := range map[string]struct {
		info   llmlocal.ManagedInfo
		state  string
		reason string
	}{
		"unsupported":   {llmlocal.ManagedInfo{State: llmlocal.ManagedUnsupported, Reason: "macOS（Apple Silicon）では llama.cpp の自動導入に対応していません。"}, "unsupported", "この PC では使えません"},
		"not installed": {llmlocal.ManagedInfo{State: llmlocal.ManagedNotInstalled, Model: "gemma-4-12b-it", ModelName: "Gemma", Reason: "llama.cpp が導入されていません。モデルマネージャーの「ローカル」で llama.cpp を導入してください"}, "not_installed", "まだ使えません"},
		"failed":        {llmlocal.ManagedInfo{State: llmlocal.ManagedFailed, ModelName: "Gemma", Reason: "llama.cpp が起動しませんでした: error: failed to load model"}, "failed", "起動に失敗しました"},
	} {
		t.Run(name, func(t *testing.T) {
			s := managedScreen(t, false, managedStub{tc.info})

			page := s.page()

			assert.Contains(t, page, `data-state="`+tc.state+`"`)
			assert.Contains(t, page, tc.reason)
			assert.Contains(t, page, `data-testid="llm-local-managed-reason"`)
			assert.Contains(t, page, template(tc.info.Reason))
		})
	}
}

// Without a managed server in the app the card has no such choice, and a request for it is refused (nothing is written).
func TestManagedCard_IsLeftOutWithoutAManagedServerAndTheChoiceIsRefused(t *testing.T) {
	s := managedScreen(t, false, nil)

	assert.NotContains(t, s.page(), "use-managed")
	rec := s.requestTo(http.MethodPut, paths.LLMLocal(), url.Values{"use_managed_present": {"1"}, "use_managed": {"1"}})

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "mam が管理する llama.cpp がありません")
	_, err := os.Stat(s.file)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestManagedCard_APinnedFileCannotChooseIt(t *testing.T) {
	s := managedScreen(t, true, gemmaStopped)

	assert.Contains(t, inputTag(t, s.page(), "use_managed"), " disabled")
	rec := s.requestTo(http.MethodPut, paths.LLMLocal(), url.Values{"use_managed": {"1"}, "use_managed_present": {"1"}})
	assert.Equal(t, http.StatusConflict, rec.Code)
}

// While it is chosen, the connection test says where the managed server stands and lists its model (it does not try the address of the form).
func TestManagedCard_TheConnectionTestSaysWhereTheServerStandsAndNeverTriesTheSavedAddress(t *testing.T) {
	s := managedScreen(t, false, gemmaStopped)
	require.Equal(t, http.StatusOK, s.requestTo(http.MethodPut, paths.LLMLocal(), url.Values{"use_managed_present": {"1"}, "use_managed": {"1"}}).Code)

	rec := s.requestTo(http.MethodPost, paths.LLMLocalTest(), url.Values{"base_url": {"http://127.0.0.1:1"}})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "この PC の llama.cpp を使います（モデル: Gemma 4 12B-it（純正・Q4_K_M））")
	assert.Contains(t, rec.Body.String(), "gemma-4-12b-it")
}

// template is the text as the page draws it (HTML-escaped).
func template(s string) string {
	return regexp.MustCompile(`[<>&"']`).ReplaceAllStringFunc(s, func(c string) string {
		return map[string]string{"<": "&lt;", ">": "&gt;", "&": "&amp;", `"`: "&#34;", "'": "&#39;"}[c]
	})
}
