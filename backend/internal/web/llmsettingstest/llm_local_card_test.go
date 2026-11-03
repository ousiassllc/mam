package llmsettingstest_test

import (
	"net"
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

// localScreen is the settings screen with the card of the local LLM server over the key store keys.
func localScreen(t *testing.T, pinned bool) (*llmScreen, *credentialtest.Memory) {
	t.Helper()
	keys := credentialtest.New()
	file := filepath.Join(t.TempDir(), "data", "llm.json")
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	st := settingsroutes.New(file, pinned, filepath.Join(t.TempDir(), "models.json"), false)
	st.LLMLocal = llmlocal.New(file, pinned, keys, nil)
	st.LLMModels = llmmodels.New(llmmodels.Options{Keys: keys, LocalBaseURL: st.LLMLocal.BaseURL})
	s := &llmScreen{t: t, file: file, engine: router.New(svc, router.Options{Settings: st})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		s.cookie = c
	}
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token = m[1]
	return s, keys
}

// inputTag is the <input> named name.
func inputTag(t *testing.T, html, name string) string {
	t.Helper()
	for _, m := range regexp.MustCompile(`<input[^>]*>`).FindAllString(html, -1) {
		if regexp.MustCompile(`\sname="` + regexp.QuoteMeta(name) + `"`).MatchString(m) {
			return m
		}
	}
	require.Failf(t, "no input", name)
	return ""
}

// ollamaServer answers GET /v1/models.
func ollamaServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gemma-4-12b"},{"id":"qwen3.8:27b"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLocalCard_IsOnTheLLMCategoryWithTheDefaultAddressAndNoKey(t *testing.T) {
	s, _ := localScreen(t, false)

	page := s.page()

	assert.Contains(t, page, `data-testid="llm-local-card"`)
	assert.Contains(t, page, "ローカル LLM サーバー（OpenAI 互換）")
	assert.Contains(t, page, `value="`+llm.LocalDefaultBaseURL+`"`)
	assert.Contains(t, page, `name="api_key"`)
	assert.Contains(t, page, `data-testid="llm-local-test"`)
	assert.Contains(t, page, `id="llm-assignment-panel"`, "the table of the assignment is still there")
	assert.NotContains(t, page, "キーあり")
}

func TestLocalCard_SaveKeepsTheAddressAndTheKeyAndNeverDrawsTheKey(t *testing.T) {
	s, keys := localScreen(t, false)
	srv := ollamaServer(t)

	rec := s.requestTo(http.MethodPut, paths.LLMLocal(), url.Values{"base_url": {srv.URL + "/v1"}, "api_key": {"sk-local-secret-1234"}, "unload_present": {"1"}, "unload": {"1"}})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, "保存しました")
	assert.Contains(t, body, `value="`+srv.URL+`"`, "the address as stored: /v1 is read as part of the server's address")
	assert.Contains(t, body, "キーあり")
	assert.Contains(t, body, "末尾 1234")
	assert.NotContains(t, body, "sk-local-secret")
	assert.Contains(t, inputTag(t, body, "unload"), " checked")
	key, err := keys.Get("local")
	require.NoError(t, err)
	assert.Equal(t, "sk-local-secret-1234", key)
	raw, err := os.ReadFile(s.file)
	require.NoError(t, err)
	assert.Contains(t, string(raw), srv.URL)
	assert.NotContains(t, string(raw), "sk-local", "the key lives in the key store, not in llm.json")
	assert.NotContains(t, s.page(), "sk-local-secret")

	again := s.requestTo(http.MethodPut, paths.LLMLocal(), url.Values{"base_url": {srv.URL}, "unload_present": {"1"}, "clear_key": {"1"}})
	require.Equal(t, http.StatusOK, again.Code)
	has, _ := keys.Has("local")
	assert.False(t, has, "the stored key was deleted")
	assert.NotContains(t, again.Body.String(), "キーあり")
}

func TestLocalCard_ARefusedAddressIs422WithTheReasonAndWritesNothing(t *testing.T) {
	s, _ := localScreen(t, false)

	rec := s.requestTo(http.MethodPut, paths.LLMLocal(), url.Values{"base_url": {"http://169.254.169.254/latest"}, "api_key": {"typed-key"}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "リンクローカル")
	assert.Contains(t, body, `aria-invalid="true"`)
	assert.Contains(t, body, "http://169.254.169.254/latest", "what was typed is drawn again")
	assert.NotContains(t, body, "typed-key", "the key is never drawn back")
	_, err := os.Stat(s.file)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestLocalCard_APinnedFileIsReadOnlyForTheAddress(t *testing.T) {
	s, keys := localScreen(t, true)

	assert.Contains(t, inputTag(t, s.page(), "base_url"), " disabled")
	rec := s.requestTo(http.MethodPut, paths.LLMLocal(), url.Values{"base_url": {"http://localhost:1234"}})
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "固定されている")

	// A save of the key alone still works: the key is not in the file.
	rec = s.requestTo(http.MethodPut, paths.LLMLocal(), url.Values{"api_key": {"only-key"}})
	require.Equal(t, http.StatusOK, rec.Code)
	got, _ := keys.Get("local")
	assert.Equal(t, "only-key", got)
}

func TestLocalCard_TheConnectionTestListsTheModels(t *testing.T) {
	s, _ := localScreen(t, false)
	srv := ollamaServer(t)

	rec := s.requestTo(http.MethodPost, paths.LLMLocalTest(), url.Values{"base_url": {srv.URL}})

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "接続できました（モデル 2 件）")
	assert.Contains(t, body, "gemma-4-12b")
	assert.Contains(t, body, "qwen3.8:27b")
}

func TestLocalCard_ADownServerShowsTheCommonErrorBox(t *testing.T) {
	s, _ := localScreen(t, false)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := "http://" + l.Addr().String()
	require.NoError(t, l.Close())

	rec := s.requestTo(http.MethodPost, paths.LLMLocalTest(), url.Values{"base_url": {addr}})

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `data-error-code="local_llm_unreachable"`)
	assert.Contains(t, body, "ローカルサーバーに接続できません")
	assert.Contains(t, body, "ベース URL")
}

func TestLocalCard_AnAddressTheRulesRefuseIsNotTried(t *testing.T) {
	s, _ := localScreen(t, false)

	rec := s.requestTo(http.MethodPost, paths.LLMLocalTest(), url.Values{"base_url": {"http://169.254.169.254"}})

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "リンクローカル")
}

// The assignment screen takes the local provider for the primary and the fallback of an Agent and saves it.
func TestLocalCard_AnAgentCanBeAssignedToTheLocalServerOnTheScreen(t *testing.T) {
	s, _ := localScreen(t, false)

	rec := s.requestTo(http.MethodPut, paths.LLMAgent("writer"), agentForm("local", "qwen3.8:27b", "local", "gemma-4-12b"))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cfg, err := llm.LoadConfig(s.file)
	require.NoError(t, err)
	assert.Equal(t, llm.Route{Primary: llm.Target{Provider: llm.ProviderLocal, Model: "qwen3.8:27b"}, Fallback: llm.Target{Provider: llm.ProviderLocal, Model: "gemma-4-12b"}}, cfg.Routes[llm.AgentWriter])
}
