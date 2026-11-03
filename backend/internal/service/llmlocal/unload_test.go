package llmlocal_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
)

// ollama is a fake Ollama: /api/ps lists the loaded models, POST /api/generate with keep_alive 0 unloads one.
type ollama struct {
	*httptest.Server
	mu       sync.Mutex
	loaded   []string
	unloaded []string
	auth     []string
}

func newOllama(t *testing.T, loaded ...string) *ollama {
	t.Helper()
	o := &ollama{loaded: loaded}
	o.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.auth = append(o.auth, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/ps":
			var models []map[string]string
			for _, m := range o.loaded {
				models = append(models, map[string]string{"name": m, "model": m})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
		case r.Method == http.MethodPost && r.URL.Path == "/api/generate":
			var body struct {
				Model     string `json:"model"`
				KeepAlive int    `json:"keep_alive"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			if body.KeepAlive != 0 || body.Model == "" {
				http.Error(w, "bad", 400)
				return
			}
			o.unloaded = append(o.unloaded, body.Model)
			_, _ = io.WriteString(w, `{"done":true,"done_reason":"unload"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(o.Close)
	return o
}

func TestBeforeGeneration_UnloadsTheOllamaModelsOnlyWhenTheSwitchIsOn(t *testing.T) {
	o := newOllama(t, "gemma3:12b", "qwen3:8b")
	path := settingsPath(t)
	svc, _ := newService(t, path, false)
	require.NoError(t, svc.Save(llmlocal.SaveInput{BaseURL: o.URL, APIKey: "k"}))

	require.NoError(t, svc.BeforeGeneration(context.Background()))
	assert.Empty(t, o.unloaded, "the switch is off: Ollama is not touched")
	assert.Empty(t, o.auth)

	on := true
	require.NoError(t, svc.Save(llmlocal.SaveInput{Unload: &on}))
	require.NoError(t, svc.BeforeGeneration(context.Background()))
	assert.Equal(t, []string{"gemma3:12b", "qwen3:8b"}, o.unloaded)
	for _, a := range o.auth {
		assert.Equal(t, "Bearer k", a)
	}
}

func TestBeforeGeneration_AServerThatIsNotOllamaIsLeftAlone(t *testing.T) {
	s, _ := server(t, 404, "not found")
	svc, _ := newService(t, settingsPath(t), false)
	on := true
	require.NoError(t, svc.Save(llmlocal.SaveInput{BaseURL: s.URL, Unload: &on}))

	assert.NoError(t, svc.BeforeGeneration(context.Background()))
}

func TestBeforeGeneration_ADownServerIsAnErrorForTheCallerToLog(t *testing.T) {
	svc, _ := newService(t, settingsPath(t), false)
	on := true
	require.NoError(t, svc.Save(llmlocal.SaveInput{BaseURL: closedAddr(t), Unload: &on}))

	assert.Error(t, svc.BeforeGeneration(context.Background()))
}
