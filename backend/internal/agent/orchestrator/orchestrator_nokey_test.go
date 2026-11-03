package orchestrator_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The failure for want of an API key tells which Agent and which providers, how to run on a ChatGPT plan, and carries the link to the
// LLM assignment; a failure of another cause carries none (#1006).
func TestOrchestrator_NoKeyFailureNamesTheAgentAndLinksToTheLLMAssignment(t *testing.T) {
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	f.llm.errs[llm.AgentProducer] = &llm.RouteError{Agent: llm.AgentProducer, Tried: []llm.TargetFailure{
		{Target: llm.Target{Provider: llm.ProviderOpenAI, Model: "gpt-5"}, Err: llm.ErrNoAPIKey},
		{Target: llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-opus-4-5"}, Err: llm.ErrNoAPIKey},
	}}

	err := f.handle(context.Background(), id)

	require.Error(t, err)
	for _, want := range []string{"Producer（コンセプト）", "openai / gpt-5（API キー未設定）", "anthropic / claude-opus-4-5（API キー未設定）", "chatgpt-subscription"} {
		assert.Contains(t, err.Error(), want)
	}
	f.chat.mu.Lock()
	defer f.chat.mu.Unlock()
	require.NotEmpty(t, f.chat.announced)
	notice := f.chat.announced[len(f.chat.announced)-1]
	assert.Equal(t, domain.RefGlobalSettings, notice.RefType, "the chat draws the link to the settings")
	assert.Equal(t, "llm", notice.RefID, "to the LLM category")
}
