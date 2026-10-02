package llm_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/credential"
)

// providerServer answers every provider's endpoint and records the last request.
type providerServer struct {
	*httptest.Server
	path    string
	headers http.Header
	body    map[string]any
}

func newProviderServer(t *testing.T, status int, response string, header http.Header) *providerServer {
	t.Helper()
	ps := &providerServer{}
	ps.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ps.path, ps.headers = r.URL.Path, r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &ps.body)
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(ps.Close)
	return ps
}

func backendFor(t *testing.T, p llm.Provider, ps *providerServer, withKey bool) llm.Backend {
	t.Helper()
	keyring.MockInit()
	store := credential.NewKeyring()
	if withKey {
		require.NoError(t, store.Set(string(p), "secret-key"))
	}
	cfg := llm.DefaultConfig()
	cfg.Providers = map[llm.Provider]llm.ProviderConfig{p: {BaseURL: ps.URL}}
	return llm.NewBackends(cfg, store, ps.Client())[p]
}

func TestBackends_Anthropic(t *testing.T) {
	ps := newProviderServer(t, 200, `{"content":[{"type":"text","text":"hello "},{"type":"tool_use"},{"type":"text","text":"world"}]}`, nil)
	b := backendFor(t, llm.ProviderAnthropic, ps, true)

	text, err := b.Complete(context.Background(), "claude-x", llm.Request{System: "sys", User: "usr", MaxTokens: 100})
	require.NoError(t, err)
	assert.Equal(t, "hello world", text)
	assert.Equal(t, "/v1/messages", ps.path)
	assert.Equal(t, "secret-key", ps.headers.Get("x-api-key"))
	assert.NotEmpty(t, ps.headers.Get("anthropic-version"))
	assert.Equal(t, "claude-x", ps.body["model"])
	assert.Equal(t, "sys", ps.body["system"])
	assert.EqualValues(t, 100, ps.body["max_tokens"])
}

func TestBackends_OpenAI(t *testing.T) {
	ps := newProviderServer(t, 200, `{"choices":[{"message":{"content":"{\"a\":1}"}}]}`, nil)
	b := backendFor(t, llm.ProviderOpenAI, ps, true)

	text, err := b.Complete(context.Background(), "gpt-x", llm.Request{System: "sys", User: "usr", MaxTokens: 50, JSON: true})
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, text)
	assert.Equal(t, "/v1/chat/completions", ps.path)
	assert.Equal(t, "Bearer secret-key", ps.headers.Get("Authorization"))
	assert.Equal(t, map[string]any{"type": "json_object"}, ps.body["response_format"])
	assert.Len(t, ps.body["messages"], 2)
}

func TestBackends_Google(t *testing.T) {
	ps := newProviderServer(t, 200, `{"candidates":[{"content":{"parts":[{"text":"a"},{"text":"b"}]}}]}`, nil)
	b := backendFor(t, llm.ProviderGoogle, ps, true)

	text, err := b.Complete(context.Background(), "gemini-x", llm.Request{System: "sys", User: "usr", MaxTokens: 50, JSON: true})
	require.NoError(t, err)
	assert.Equal(t, "ab", text)
	assert.Equal(t, "/v1beta/models/gemini-x:generateContent", ps.path)
	assert.Equal(t, "secret-key", ps.headers.Get("x-goog-api-key"))
	gen, _ := ps.body["generationConfig"].(map[string]any)
	assert.Equal(t, "application/json", gen["responseMimeType"])
}

func TestBackends_MissingKeyMakesNoRequest(t *testing.T) {
	ps := newProviderServer(t, 200, `{}`, nil)
	b := backendFor(t, llm.ProviderAnthropic, ps, false)

	_, err := b.Complete(context.Background(), "m", llm.Request{})
	assert.ErrorIs(t, err, llm.ErrNoAPIKey)
	assert.Empty(t, ps.path, "no request was sent")
}

func TestBackends_HTTPErrorsBecomeAPIErrors(t *testing.T) {
	ps := newProviderServer(t, 429, `{"error":"slow down"}`, http.Header{"Retry-After": {"7"}})
	b := backendFor(t, llm.ProviderOpenAI, ps, true)

	_, err := b.Complete(context.Background(), "m", llm.Request{})
	var api *llm.APIError
	require.ErrorAs(t, err, &api)
	assert.Equal(t, 429, api.Status)
	assert.Equal(t, 7*time.Second, api.RetryAfter)
	assert.Contains(t, api.Message, "slow down")
	assert.NotContains(t, err.Error(), "secret-key")
}

func TestBackends_EmptyAnswerIsAnError(t *testing.T) {
	ps := newProviderServer(t, 200, `{"choices":[]}`, nil)
	b := backendFor(t, llm.ProviderOpenAI, ps, true)

	_, err := b.Complete(context.Background(), "m", llm.Request{})
	require.Error(t, err)
}

func TestBackends_NetworkFailureIsAnAPIErrorWithoutURL(t *testing.T) {
	ps := newProviderServer(t, 200, `{}`, nil)
	b := backendFor(t, llm.ProviderOpenAI, ps, true)
	ps.Close()

	_, err := b.Complete(context.Background(), "m", llm.Request{})
	var api *llm.APIError
	require.ErrorAs(t, err, &api)
	assert.Zero(t, api.Status)
	assert.NotContains(t, err.Error(), ps.URL)
}

func TestLoadConfig_DefaultsCoverEveryAgentWithDistinctPrimaryAndFallback(t *testing.T) {
	cfg, err := llm.LoadConfig("")
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
	for agent, r := range cfg.Routes {
		assert.NotEqual(t, r.Primary.Provider, r.Fallback.Provider, "%s: the alternative is a different provider", agent)
	}
	// model-adapters.md §2
	assert.Equal(t, llm.ProviderOpenAI, cfg.Routes[llm.AgentProducer].Primary.Provider)
	assert.Equal(t, llm.ProviderAnthropic, cfg.Routes[llm.AgentDirector].Primary.Provider)
	assert.Equal(t, llm.ProviderGoogle, cfg.Routes[llm.AgentDirector].Fallback.Provider)
}

func TestLoadConfig_FileOverridesOnlyWhatItSets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"routes": {"writer": {"primary": {"provider": "google", "model": "g-1"}, "fallback": {"provider": "openai", "model": "o-1"}}},
		"retry": {"max_attempts": 5, "base_delay_ms": 10, "max_delay_ms": 100}
	}`), 0o600))

	cfg, err := llm.LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, llm.Target{Provider: llm.ProviderGoogle, Model: "g-1"}, cfg.Routes[llm.AgentWriter].Primary)
	assert.Equal(t, 5, cfg.Retry.MaxAttempts)
	assert.Equal(t, llm.DefaultConfig().Routes[llm.AgentProducer], cfg.Routes[llm.AgentProducer], "other routes keep their default")
	assert.Equal(t, llm.DefaultConfig().TimeoutSeconds, cfg.TimeoutSeconds)
}

func TestLoadConfig_RejectsBadFiles(t *testing.T) {
	for name, content := range map[string]string{
		"unknown key":          `{"route": {}}`,
		"unknown provider":     `{"routes": {"writer": {"primary": {"provider": "acme", "model": "m"}, "fallback": {"provider": "openai", "model": "m"}}}}`,
		"empty model":          `{"routes": {"writer": {"primary": {"provider": "openai", "model": ""}, "fallback": {"provider": "google", "model": "m"}}}}`,
		"unknown agent":        `{"routes": {"writter": {"primary": {"provider": "openai", "model": "m"}, "fallback": {"provider": "google", "model": "m"}}}}`,
		"unknown provider key": `{"providers": {"opneai": {"base_url": "http://x"}}}`,
		"bad retry":            `{"retry": {"max_attempts": 0, "base_delay_ms": 1, "max_delay_ms": 1}}`,
		"not json":             `nope`,
	} {
		path := filepath.Join(t.TempDir(), "llm.json")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		_, err := llm.LoadConfig(path)
		assert.Error(t, err, name)
	}
	_, err := llm.LoadConfig(filepath.Join(t.TempDir(), "missing.json"))
	assert.Error(t, err)
}

// images are what the AI Video Judge attaches: a captioned reference and a frame.
var images = []llm.Image{
	{MimeType: "image/png", Data: []byte("ref"), Caption: "Reference image"},
	{MimeType: "image/jpeg", Data: []byte("frame")},
}

func TestBackends_Anthropic_SendsImagesAfterTheText(t *testing.T) {
	ps := newProviderServer(t, 200, `{"content":[{"type":"text","text":"ok"}]}`, nil)
	b := backendFor(t, llm.ProviderAnthropic, ps, true)

	_, err := b.Complete(context.Background(), "claude-x", llm.Request{System: "sys", User: "usr", Images: images})
	require.NoError(t, err)
	msgs, _ := ps.body["messages"].([]any)
	require.Len(t, msgs, 1)
	blocks, _ := msgs[0].(map[string]any)["content"].([]any)
	require.Len(t, blocks, 4, "user text, caption, two images (the frame has no caption)")
	assert.Equal(t, "usr", blocks[0].(map[string]any)["text"])
	assert.Equal(t, "Reference image", blocks[1].(map[string]any)["text"])
	src := blocks[2].(map[string]any)["source"].(map[string]any)
	assert.Equal(t, "image/png", src["media_type"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("ref")), src["data"])
	assert.Equal(t, "image", blocks[3].(map[string]any)["type"])
}

func TestBackends_OpenAI_SendsImagesAsDataURLs(t *testing.T) {
	ps := newProviderServer(t, 200, `{"choices":[{"message":{"content":"ok"}}]}`, nil)
	b := backendFor(t, llm.ProviderOpenAI, ps, true)

	_, err := b.Complete(context.Background(), "gpt-x", llm.Request{System: "sys", User: "usr", Images: images})
	require.NoError(t, err)
	msgs, _ := ps.body["messages"].([]any)
	require.Len(t, msgs, 2)
	parts, _ := msgs[1].(map[string]any)["content"].([]any)
	require.Len(t, parts, 4)
	url := parts[2].(map[string]any)["image_url"].(map[string]any)["url"]
	assert.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("ref")), url)
}

func TestBackends_Google_SendsImagesAsInlineData(t *testing.T) {
	ps := newProviderServer(t, 200, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`, nil)
	b := backendFor(t, llm.ProviderGoogle, ps, true)

	_, err := b.Complete(context.Background(), "gemini-x", llm.Request{System: "sys", User: "usr", Images: images})
	require.NoError(t, err)
	contents, _ := ps.body["contents"].([]any)
	parts, _ := contents[0].(map[string]any)["parts"].([]any)
	require.Len(t, parts, 4)
	inline := parts[2].(map[string]any)["inline_data"].(map[string]any)
	assert.Equal(t, "image/png", inline["mime_type"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("ref")), inline["data"])
}

func TestBackends_TextOnlyRequestsKeepThePlainUserMessage(t *testing.T) {
	ps := newProviderServer(t, 200, `{"content":[{"type":"text","text":"ok"}]}`, nil)
	b := backendFor(t, llm.ProviderAnthropic, ps, true)

	_, err := b.Complete(context.Background(), "claude-x", llm.Request{User: "usr"})
	require.NoError(t, err)
	msgs, _ := ps.body["messages"].([]any)
	assert.Equal(t, "usr", msgs[0].(map[string]any)["content"])
}

func TestDefaultClientDoesNotForwardTheKeyOnARedirectToAnotherHost(t *testing.T) {
	var foreignHits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { foreignHits.Add(1) }))
	defer foreign.Close()
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/steal", http.StatusFound)
	}))
	defer vendor.Close()
	keyring.MockInit()
	store := credential.NewKeyring()
	for _, p := range []llm.Provider{llm.ProviderAnthropic, llm.ProviderOpenAI, llm.ProviderGoogle} {
		require.NoError(t, store.Set(string(p), "secret-key"))
	}
	cfg := llm.DefaultConfig()
	for p := range cfg.Providers {
		cfg.Providers[p] = llm.ProviderConfig{BaseURL: vendor.URL}
	}
	backends := llm.NewBackends(cfg, store, nil)

	for p, b := range backends {
		_, err := b.Complete(context.Background(), "m", llm.Request{System: "s", User: "u", MaxTokens: 10})
		require.Error(t, err, p)
	}
	assert.Zero(t, foreignHits.Load())
}
