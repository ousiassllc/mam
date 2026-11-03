package apicheck_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

type listed struct {
	ids     []string
	headers http.Header
	uri     string
}

func listFrom(t *testing.T, p llm.Provider, status int, body string) (listed, error) {
	t.Helper()
	var headers http.Header
	var uri string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers, uri = r.Header.Clone(), r.URL.RequestURI()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	ids, err := llm.ListModels(context.Background(), srv.Client(), p, srv.URL+"/", "secret-key")
	return listed{ids, headers, uri}, err
}

func TestListModels_Anthropic(t *testing.T) {
	got, err := listFrom(t, llm.ProviderAnthropic, 200, `{"data":[{"id":"claude-b"},{"id":"claude-a"},{"id":"other"}]}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"claude-b", "claude-a"}, got.ids, "the vendor's order, text models only")
	assert.Equal(t, "secret-key", got.headers.Get("x-api-key"))
	assert.NotEmpty(t, got.headers.Get("anthropic-version"))
	assert.Equal(t, "/v1/models?limit=1000", got.uri)
}

func TestListModels_OpenAIKeepsTextModelsNewestFirst(t *testing.T) {
	got, err := listFrom(t, llm.ProviderOpenAI, 200, `{"data":[
		{"id":"gpt-old","created":1},{"id":"text-embedding-3-large","created":9},{"id":"gpt-image-1","created":9},
		{"id":"whisper-1","created":9},{"id":"gpt-new","created":5},{"id":"o3-mini","created":3},{"id":"gpt-4o-realtime-preview","created":9},
		{"id":"gpt-4o-mini-tts","created":9},{"id":"omni-moderation-latest","created":9}]}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-new", "o3-mini", "gpt-old"}, got.ids)
	assert.Equal(t, "Bearer secret-key", got.headers.Get("Authorization"))
}

func TestListModels_GoogleKeepsGenerateContentGeminis(t *testing.T) {
	got, err := listFrom(t, llm.ProviderGoogle, 200, `{"models":[
		{"name":"models/gemini-x","supportedGenerationMethods":["generateContent","countTokens"]},
		{"name":"models/embedding-001","supportedGenerationMethods":["embedContent"]},
		{"name":"models/gemini-embedding","supportedGenerationMethods":["embedContent"]},
		{"name":"models/gemini-y-image","supportedGenerationMethods":["generateContent"]},
		{"name":"models/gemini-z","supportedGenerationMethods":["generateContent"]}]}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"gemini-x", "gemini-z"}, got.ids)
	assert.Equal(t, "secret-key", got.headers.Get("x-goog-api-key"))
}

func TestListModels_FailuresNeverCarryTheKey(t *testing.T) {
	_, err := listFrom(t, llm.ProviderOpenAI, 401, `{"error":"bad key"}`)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-key")
	assert.Contains(t, err.Error(), "401")

	_, err = listFrom(t, llm.ProviderGoogle, 200, `not json`)
	require.Error(t, err)

	_, err = llm.ListModels(context.Background(), nil, llm.ProviderChatGPTCodex, "http://x", "k")
	assert.Error(t, err, "a provider without a model list API")
}
