package localllm_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// The section "別のサーバーにつなぐ" says that the address is not in force while a server of mam is chosen (the fields stay enabled: a disabled field is not sent).
func TestConnection_SaysThatTheAddressIsNotInForceWhileAServerOfMamIsChosen(t *testing.T) {
	s := llmScreen(t, &server{info: stopped}, nil)
	assert.NotContains(t, s.PageOf(paths.MMLocalLLM), "mm-llm-connection-note")

	require.Equal(t, http.StatusOK, s.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), useForm("llama-cpp")).Code)
	page := s.PageOf(paths.MMLocalLLM)

	assert.Contains(t, page, `data-testid="mm-llm-connection-note"`)
	assert.Contains(t, page, "今は別のサーバーを使っています")
	assert.NotRegexp(t, `<input[^>]*name="base_url"[^>]* disabled`, page)
}

// fake is a server that answers GET /v1/models, and counts the requests.
func fake(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gemma-4-12b"},{"id":"qwen3.8:27b"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
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

// The save is the save of the card of the settings (#1008) at its new place: the same fields, status codes and words; the key is kept in the key store and never drawn.
func TestConnection_SaveKeepsTheAddressAndTheKeyAndNeverDrawsTheKey(t *testing.T) {
	s := llmScreen(t, nil, nil)
	srv, _ := fake(t)

	rec := s.Do(http.MethodPut, paths.ModelManagerLocalLLMConnection(), url.Values{"base_url": {srv.URL + "/v1"}, "api_key": {"sk-local-secret-1234"}, "unload_present": {"1"}, "unload": {"1"}})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, "保存しました")
	assert.Contains(t, body, `value="`+srv.URL+`"`, "the address as stored: /v1 is read as part of the server's address")
	assert.Contains(t, body, "キーあり")
	assert.Contains(t, body, "末尾 1234")
	assert.NotContains(t, body, "sk-local-secret")
	assert.Contains(t, inputTag(t, body, "unload"), " checked")
	assert.Equal(t, 1, count(body, "mm-llm-connection"), "the answer is the section alone")
	assert.Zero(t, count(body, "mm-llm-use"))
	key, err := s.LLMKeys.Get("local")
	require.NoError(t, err)
	assert.Equal(t, "sk-local-secret-1234", key)
	raw, err := os.ReadFile(s.LLMFile)
	require.NoError(t, err)
	assert.Contains(t, string(raw), srv.URL)
	assert.NotContains(t, string(raw), "sk-local", "the key lives in the key store, not in llm.json")
	assert.NotContains(t, s.PageOf(paths.MMLocalLLM), "sk-local-secret")
	assert.NotContains(t, s.Page(), "sk-local-secret")

	again := s.Do(http.MethodPut, paths.ModelManagerLocalLLMConnection(), url.Values{"base_url": {srv.URL}, "unload_present": {"1"}, "clear_key": {"1"}})
	require.Equal(t, http.StatusOK, again.Code)
	has, _ := s.LLMKeys.Has("local")
	assert.False(t, has, "the stored key was deleted")
	assert.NotContains(t, again.Body.String(), "キーあり")
}

// A save of the address does not change the server in use.
func TestConnection_SaveDoesNotChangeTheServerInUse(t *testing.T) {
	s := llmScreen(t, &server{info: stopped}, nil)
	require.Equal(t, http.StatusOK, s.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), useForm("llama-cpp")).Code)

	rec := s.Do(http.MethodPut, paths.ModelManagerLocalLLMConnection(), url.Values{"base_url": {"http://localhost:1234"}, "unload_present": {"1"}})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, providerOf(t, s).UseManagedRuntime)
	assert.Equal(t, "http://localhost:1234", providerOf(t, s).BaseURL)
}

func TestConnection_ARefusedAddressIs422WithTheReasonAndWritesNothing(t *testing.T) {
	s := llmScreen(t, nil, nil)

	rec := s.Do(http.MethodPut, paths.ModelManagerLocalLLMConnection(), url.Values{"base_url": {"http://169.254.169.254/latest"}, "api_key": {"typed-key"}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "リンクローカル")
	assert.Contains(t, body, `aria-invalid="true"`)
	assert.Contains(t, body, "http://169.254.169.254/latest", "what was typed is drawn again")
	assert.NotContains(t, body, "typed-key", "the key is never drawn back")
	assert.NoFileExists(t, s.LLMFile)
}

func TestConnection_APinnedFileIsReadOnlyForTheAddressButTheKeyCanBeSaved(t *testing.T) {
	s := screentest.NewWithLLM(t, true)

	assert.Contains(t, inputTag(t, s.PageOf(paths.MMLocalLLM), "base_url"), " disabled")
	rec := s.Do(http.MethodPut, paths.ModelManagerLocalLLMConnection(), url.Values{"base_url": {"http://localhost:1234"}})
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "固定されている")

	rec = s.Do(http.MethodPut, paths.ModelManagerLocalLLMConnection(), url.Values{"api_key": {"only-key"}})
	require.Equal(t, http.StatusOK, rec.Code)
	got, _ := s.LLMKeys.Get("local")
	assert.Equal(t, "only-key", got)
}

func TestConnectionTest_ListsTheModelsOfTheFormsServer(t *testing.T) {
	s := llmScreen(t, nil, nil)
	srv, _ := fake(t)

	rec := s.Do(http.MethodPost, paths.ModelManagerLocalLLMConnectionTest(), url.Values{"base_url": {srv.URL}})

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "接続できました（モデル 2 件）")
	assert.Contains(t, body, "gemma-4-12b")
	assert.Contains(t, body, "qwen3.8:27b")
	assert.Contains(t, body, `role="status"`)
}

func TestConnectionTest_ADownServerShowsTheCommonErrorBox(t *testing.T) {
	s := llmScreen(t, nil, nil)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := "http://" + l.Addr().String()
	require.NoError(t, l.Close())

	rec := s.Do(http.MethodPost, paths.ModelManagerLocalLLMConnectionTest(), url.Values{"base_url": {addr}})

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `data-error-code="local_llm_unreachable"`)
	assert.Contains(t, body, "ローカルサーバーに接続できません")
	assert.Contains(t, body, "ベース URL")
}

func TestConnectionTest_AnAddressTheRulesRefuseIsNotTried(t *testing.T) {
	s := llmScreen(t, nil, nil)

	rec := s.Do(http.MethodPost, paths.ModelManagerLocalLLMConnectionTest(), url.Values{"base_url": {"http://169.254.169.254"}})

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "リンクローカル")
}

// While a server of mam is in use the test does not try the address of the form (no request reaches the fake server): it says where that server stands. Not for llama.cpp only: Ollama too.
func TestConnectionTest_WhileAServerOfMamIsInUseItSaysWhereItStandsAndNeverTriesTheForm(t *testing.T) {
	for name, tc := range map[string]struct{ use, who string }{
		"llama.cpp": {"llama-cpp", "この PC の llama.cpp を使います（モデル: Gemma）"},
		"ollama":    {"ollama", "Ollama を使います（モデル: Gemma）"},
	} {
		t.Run(name, func(t *testing.T) {
			s := llmScreen(t, &server{info: stopped}, &server{info: stopped})
			srv, requests := fake(t)
			require.Equal(t, http.StatusOK, s.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), useForm(tc.use)).Code)

			rec := s.Do(http.MethodPost, paths.ModelManagerLocalLLMConnectionTest(), url.Values{"base_url": {srv.URL}})

			require.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.who)
			assert.Contains(t, rec.Body.String(), "gemma-4-12b-it")
			assert.Zero(t, requests.Load(), "the form's address was not tried")
		})
	}
}

func TestConnectionTest_AServerOfMamThatCannotRunIsTheCommonErrorBox(t *testing.T) {
	s := llmScreen(t, &server{info: llmlocal.ManagedInfo{State: llmlocal.ManagedFailed, Reason: "llama.cpp が起動しませんでした"}}, nil)
	require.Equal(t, http.StatusOK, s.Do(http.MethodPut, paths.ModelManagerLocalLLMUse(), useForm("llama-cpp")).Code)

	rec := s.Do(http.MethodPost, paths.ModelManagerLocalLLMConnectionTest(), nil)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-error-code="local_llm_unreachable"`)
	assert.Contains(t, rec.Body.String(), "llama.cpp が起動しませんでした", "the reason is the detail of the box")
	assert.Contains(t, rec.Body.String(), "モデルマネージャーの「ローカル LLM」", "the remedy points where the address is now")
}
