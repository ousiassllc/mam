package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/localllm"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// localLLMServer is a fake OpenAI-compatible local server (what Ollama, LM Studio and llama.cpp server look like from mam). It answers each Agent by the
// name in the system prompt, and behaves like the servers that make a local model hard to use: it refuses response_format and chat_template_kwargs, a
// reasoning model writes <think>…</think> before its answer, one answer comes in a code fence and one is not JSON at the first try.
type localLLMServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []localRequest
	writes   int
}

type localRequest struct {
	agent string
	body  map[string]any
	user  string
}

func newLocalLLMServer(t *testing.T) *localLLMServer {
	t.Helper()
	s := &localLLMServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.Unmarshal(raw, &body))
		var generic map[string]any
		require.NoError(t, json.Unmarshal(raw, &generic))
		var system, user string
		for _, m := range body.Messages {
			if m.Role == "system" {
				system = m.Content
			} else {
				user = m.Content
			}
		}
		if generic["response_format"] != nil || generic["chat_template_kwargs"] != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"unknown field response_format / chat_template_kwargs"}}`)
			return
		}
		agent, answer := s.answer(system, user)
		s.mu.Lock()
		s.requests = append(s.requests, localRequest{agent: agent, body: generic, user: user})
		s.mu.Unlock()
		content, _ := json.Marshal(answer)
		_, _ = io.WriteString(w, fmt.Sprintf(`{"choices":[{"message":{"content":%s}}],"usage":{"prompt_tokens":11,"completion_tokens":5}}`, content))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *localLLMServer) answer(system, _ string) (agent, text string) {
	switch {
	case strings.Contains(system, "Producer Agent"):
		return "producer", "<think>The brief is a CM for a serum. {Let me think}.</think>\n" + conceptJSON
	case strings.Contains(system, "Writer Agent"):
		s.mu.Lock()
		s.writes++
		first := s.writes == 1
		s.mu.Unlock()
		if first {
			return "writer", "了解しました。脚本を書きます。"
		}
		return "writer", scriptJSON
	case strings.Contains(system, "Director Agent"):
		return "director", "```json\n" + planJSON + "\n```"
	case strings.Contains(system, "Storyboard Agent"):
		return "storyboard", boardJSON
	}
	return "unknown", "{}"
}

func (s *localLLMServer) seen() []localRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]localRequest(nil), s.requests...)
}

// #1008: the planning (concept, script, shot plan, storyboard) runs to the end when every Agent is assigned to a local OpenAI-compatible server.
func TestHandle_PlanningRunsToTheEndOnALocalOpenAICompatibleServer(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	srv := newLocalLLMServer(t)
	cfg := llm.DefaultConfig()
	cfg.Providers[llm.ProviderLocal] = llm.ProviderConfig{BaseURL: srv.URL + "/v1"}
	for _, a := range llm.Agents() {
		cfg.Routes[a] = llm.Route{Primary: llm.Target{Provider: llm.ProviderLocal, Model: "qwen3.8:27b"}, Fallback: llm.Target{Provider: llm.ProviderLocal, Model: "gemma-4-12b"}}
	}
	require.NoError(t, cfg.Validate())
	keys := credentialtest.New()
	backends := llm.NewBackends(cfg, keys, nil)
	localllm.Add(backends, keys, nil, func() string { return cfg.Providers[llm.ProviderLocal].BaseURL })
	client := llm.NewClient(cfg, backends)
	f.orch = orchestrator.New(orchestrator.Deps{
		Projects: f.projects, Plans: f.plans, ChatRepo: f.chatRepo, Chat: f.chat, Jobs: f.jobs, Notes: f.notes, LLM: client,
	})
	id := f.project(t, seed{brief: completeBrief(), level: domain.ConfirmFullAuto})

	require.NoError(t, f.handle(ctx, id))

	assert.Equal(t, domain.StateVisualDesign, f.state(t, id))
	script, err := f.plans.LatestScript(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "脚本タイトル", script.Doc.Script.Title, "the second answer of the Writer, asked again after the first was not JSON")
	assert.Equal(t, "朝の一滴", script.Doc.Concept.Title, "the concept after the <think> block was cut off")
	scenes, err := f.plans.ListScenes(ctx, id)
	require.NoError(t, err)
	require.Len(t, scenes, 1)
	shots, err := f.plans.ListShots(ctx, scenes[0].ID)
	require.NoError(t, err)
	assert.Len(t, shots, 3, "the plan in a code fence was read")

	var agents []string
	var retry localRequest
	for _, r := range srv.seen() {
		agents = append(agents, r.agent)
		assert.NotContains(t, r.body, "response_format", "a server that refused it is not asked again")
		assert.NotContains(t, r.body, "chat_template_kwargs")
		assert.Equal(t, "qwen3.8:27b", r.body["model"])
		if r.agent == "writer" {
			retry = r
		}
	}
	assert.Equal(t, []string{"producer", "writer", "writer", "director", "storyboard"}, agents)
	assert.Contains(t, retry.user, "could not be read as JSON", "the asked-again prompt says why")
}
