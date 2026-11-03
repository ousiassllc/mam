package localllm_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/localllm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

type localServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []map[string]any
	headers  []http.Header
	// reply is the status and body of the n-th chat request (0-based) given the request.
	reply func(n int, body map[string]any) (int, string)
}

func newLocalServer(t *testing.T, reply func(n int, body map[string]any) (int, string)) *localServer {
	t.Helper()
	s := &localServer{reply: reply}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			s.mu.Lock()
			s.headers = append(s.headers, r.Header.Clone())
			s.mu.Unlock()
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"gemma-4-12b"},{"id":"nomic-embed-text"},{"id":"qwen3.8:27b"}]}`)
		case "/v1/chat/completions":
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			require.NoError(t, json.Unmarshal(raw, &body))
			s.mu.Lock()
			n := len(s.requests)
			s.requests = append(s.requests, body)
			s.headers = append(s.headers, r.Header.Clone())
			s.mu.Unlock()
			status, text := s.reply(n, body)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, text)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *localServer) bodies() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.requests...)
}

func chatAnswer(content string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"content": content}}},
		"usage":   map[string]any{"prompt_tokens": 7, "completion_tokens": 3},
	})
	return string(b)
}

func localBackendFor(t *testing.T, baseURL string, key string) (llm.Backend, credential.Store) {
	t.Helper()
	store := credentialtest.New()
	if key != "" {
		require.NoError(t, store.Set(string(llm.ProviderLocal), key))
	}
	backends := map[llm.Provider]llm.Backend{}
	localllm.Add(backends, store, nil, func() string { return baseURL })
	return backends[llm.ProviderLocal], store
}

func TestLocalBackend_CallsChatCompletionsWithoutAKey(t *testing.T) {
	s := newLocalServer(t, func(int, map[string]any) (int, string) { return 200, chatAnswer("こんにちは") })
	b, _ := localBackendFor(t, s.URL, "")

	done, err := b.Complete(context.Background(), "gemma-4-12b", llm.Request{System: "sys", User: "usr", MaxTokens: 100})

	require.NoError(t, err)
	assert.Equal(t, "こんにちは", done.Text)
	assert.Equal(t, &llm.Usage{InputTokens: 7, OutputTokens: 3}, done.Usage)
	body := s.bodies()[0]
	assert.Equal(t, "gemma-4-12b", body["model"])
	assert.EqualValues(t, 100, body["max_tokens"])
	assert.NotContains(t, body, "response_format", "a role that does not read JSON does not ask for the JSON mode")
	assert.NotContains(t, body, "chat_template_kwargs")
	assert.Empty(t, s.headers[0].Get("Authorization"), "no key stored: no Authorization header")
}

func TestLocalBackend_SendsTheOptionalKeyOnlyToTheServer(t *testing.T) {
	s := newLocalServer(t, func(int, map[string]any) (int, string) { return 200, chatAnswer("ok") })
	b, _ := localBackendFor(t, s.URL, "local-secret")

	_, err := b.Complete(context.Background(), "m", llm.Request{User: "u"})

	require.NoError(t, err)
	assert.Equal(t, "Bearer local-secret", s.headers[0].Get("Authorization"))
}

func TestLocalBackend_JSONRoleAsksForTheJSONModeAndNoThinking(t *testing.T) {
	s := newLocalServer(t, func(int, map[string]any) (int, string) { return 200, chatAnswer(`{"a":1}`) })
	b, _ := localBackendFor(t, s.URL, "")

	_, err := b.Complete(context.Background(), "m", llm.Request{User: "u", JSON: true})

	require.NoError(t, err)
	body := s.bodies()[0]
	assert.Equal(t, map[string]any{"type": "json_object"}, body["response_format"])
	assert.Equal(t, map[string]any{"enable_thinking": false}, body["chat_template_kwargs"])
}

func TestLocalBackend_ALeftOutFieldIsRetriedWithoutItAndRemembered(t *testing.T) {
	// The server refuses response_format, then chat_template_kwargs: both are left out, and the answer comes at the third try.
	s := newLocalServer(t, func(_ int, body map[string]any) (int, string) {
		switch {
		case body["response_format"] != nil:
			return 400, `{"error":{"message":"response_format is not supported by this server"}}`
		case body["chat_template_kwargs"] != nil:
			return 400, `{"error":{"message":"unknown field chat_template_kwargs"}}`
		}
		return 200, chatAnswer(`{"ok":true}`)
	})
	b, _ := localBackendFor(t, s.URL, "")

	done, err := b.Complete(context.Background(), "m", llm.Request{User: "u", JSON: true})
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, done.Text)
	require.Len(t, s.bodies(), 3)
	assert.NotContains(t, s.bodies()[2], "response_format")
	assert.NotContains(t, s.bodies()[2], "chat_template_kwargs")

	// The next JSON call of the same model does not ask for them again.
	_, err = b.Complete(context.Background(), "m", llm.Request{User: "u", JSON: true})
	require.NoError(t, err)
	require.Len(t, s.bodies(), 4)
	assert.NotContains(t, s.bodies()[3], "response_format")
	assert.NotContains(t, s.bodies()[3], "chat_template_kwargs")
}

func TestLocalBackend_ARefusalThatIsNotAboutAFieldIsReturned(t *testing.T) {
	s := newLocalServer(t, func(int, map[string]any) (int, string) {
		return 404, `{"error":{"message":"model 'nope' not found"}}`
	})
	b, _ := localBackendFor(t, s.URL, "")

	_, err := b.Complete(context.Background(), "nope", llm.Request{User: "u"})

	var api *llm.APIError
	require.ErrorAs(t, err, &api)
	assert.Equal(t, 404, api.Status)
	assert.Contains(t, api.Message, "not found")
	assert.Len(t, s.bodies(), 1)
}

func TestLocalBackend_ARefusedPlainRequestIsNotRetriedWithoutFields(t *testing.T) {
	s := newLocalServer(t, func(int, map[string]any) (int, string) { return 400, `{"error":{"message":"context length exceeded"}}` })
	b, _ := localBackendFor(t, s.URL, "")

	_, err := b.Complete(context.Background(), "m", llm.Request{User: "u"})

	require.Error(t, err)
	assert.Len(t, s.bodies(), 1)
}

func TestLocalBackend_CutsOffTheThinkingOfTheAnswer(t *testing.T) {
	s := newLocalServer(t, func(int, map[string]any) (int, string) {
		return 200, chatAnswer("<think>Let me {think} about it.</think>\n{\"title\":\"x\"}")
	})
	b, _ := localBackendFor(t, s.URL, "")

	done, err := b.Complete(context.Background(), "deepseek-r1", llm.Request{User: "u", JSON: true})

	require.NoError(t, err)
	assert.Equal(t, `{"title":"x"}`, done.Text)
}

func TestLocalBackend_AnAnswerThatIsOnlyThinkingIsEmpty(t *testing.T) {
	s := newLocalServer(t, func(int, map[string]any) (int, string) {
		return 200, chatAnswer("<think>still thinking and the tokens ran out")
	})
	b, _ := localBackendFor(t, s.URL, "")

	_, err := b.Complete(context.Background(), "m", llm.Request{User: "u"})

	var api *llm.APIError
	require.ErrorAs(t, err, &api)
	assert.Contains(t, api.Message, "no text")
}

func TestStripThinking(t *testing.T) {
	for in, want := range map[string]string{
		"plain":                               "plain",
		"<think>a</think>b":                   "b",
		"x<think>a</think>y<think>b</think>z": "xyz",
		"thinking without an opening</think>answer": "answer",
		"answer<think>cut off":                      "answer",
	} {
		assert.Equal(t, want, localllm.StripThinking(in), in)
	}
}

func TestListModels_Local(t *testing.T) {
	s := newLocalServer(t, nil)

	ids, err := localllm.ListModels(context.Background(), nil, s.URL+"/v1/", "k")

	require.NoError(t, err)
	assert.Equal(t, []string{"gemma-4-12b", "qwen3.8:27b"}, ids, "the embedding model is not offered; a /v1 at the end of the address is read as the address")
	assert.Equal(t, "Bearer k", s.headers[0].Get("Authorization"))

	_, err = localllm.ListModels(context.Background(), nil, s.URL, "")
	require.NoError(t, err)
	assert.Empty(t, s.headers[1].Get("Authorization"))
}

func TestNormalizeLocalBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:11434":      "http://localhost:11434",
		" http://localhost:11434/ ":   "http://localhost:11434",
		"http://localhost:1234/v1":    "http://localhost:1234",
		"http://localhost:1234/v1/":   "http://localhost:1234",
		"http://192.168.1.20:8080":    "http://192.168.1.20:8080",
		"http://100.64.1.2:11434":     "http://100.64.1.2:11434",
		"https://llm.example.net/api": "https://llm.example.net/api",
	} {
		got, err := localllm.NormalizeBaseURL(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for in, reason := range map[string]error{
		"":                             localcfg.ErrBaseURLShape,
		"localhost:11434":              localcfg.ErrBaseURLShape,
		"ftp://localhost":              localcfg.ErrBaseURLScheme,
		"file:///etc/passwd":           localcfg.ErrBaseURLShape,
		"http://user:pw@localhost:80":  localcfg.ErrBaseURLParts,
		"http://localhost:80?x=1":      localcfg.ErrBaseURLParts,
		"http://localhost:80#frag":     localcfg.ErrBaseURLParts,
		"http://169.254.169.254/":      localcfg.ErrBaseURLLinkLocal,
		"http://[fe80::1]:11434":       localcfg.ErrBaseURLLinkLocal,
		"http://0.0.0.0:11434":         localcfg.ErrBaseURLLinkLocal,
		"http://[::ffff:169.254.1.1]/": localcfg.ErrBaseURLLinkLocal,
	} {
		_, err := localllm.NormalizeBaseURL(in)
		assert.ErrorIs(t, err, reason, in)
	}
}

func TestTheDefaultClientRefusesALinkLocalAddressAtConnectTime(t *testing.T) {
	// The settings check refuses a literal address; this is the guard that stops it whatever name it is reached by.
	b, _ := localBackendFor(t, "http://169.254.169.254", "")

	_, err := b.Complete(context.Background(), "m", llm.Request{User: "u"})

	require.ErrorIs(t, err, safefetch.ErrRefused)
}

func TestIsLocalUnreachable(t *testing.T) {
	// A closed port: the connection is refused.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	b, _ := localBackendFor(t, "http://"+addr, "")

	_, err = b.Complete(context.Background(), "m", llm.Request{User: "u"})

	require.Error(t, err)
	assert.True(t, localllm.IsUnreachable(err))
	var api *llm.APIError
	require.ErrorAs(t, err, &api)
	assert.Equal(t, llm.ProviderLocal, api.Provider)
	assert.Zero(t, api.Status)

	assert.False(t, localllm.IsUnreachable(&llm.APIError{Provider: llm.ProviderLocal, Status: 500, Message: "boom"}))
	assert.False(t, localllm.IsUnreachable(&llm.APIError{Provider: llm.ProviderOpenAI, Err: errors.New("refused")}))
	assert.False(t, localllm.IsUnreachable(nil))
}
