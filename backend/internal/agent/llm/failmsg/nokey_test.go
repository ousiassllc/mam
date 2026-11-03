package failmsg_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/failmsg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// backend answers every call with err.
type backend struct{ err error }

func (b backend) Complete(context.Context, string, llm.Request) (llm.Completion, error) {
	return llm.Completion{}, b.err
}

// The failure for want of a key names the Agent and the targets that were tried, how to use a ChatGPT plan instead, and a
// setting that waits for a restart; it is still an ErrNoAPIKey for the callers that look for one.
func TestNoKeyMessage_NamesTheAgentTheTargetsAndTheWayOut(t *testing.T) {
	noKey := backend{fmt.Errorf("%w: openai", llm.ErrNoAPIKey)}
	other := backend{fmt.Errorf("%w: anthropic", llm.ErrNoAPIKey)}
	client := llm.NewClient(llm.DefaultConfig(), map[llm.Provider]llm.Backend{
		llm.ProviderOpenAI: noKey, llm.ProviderAnthropic: other, llm.ProviderGoogle: noKey,
	})
	client.Apply(func() llm.Config { c := llm.DefaultConfig(); c.TimeoutSeconds = 99; return c }())

	_, err := client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.ErrorIs(t, err, llm.ErrNoAPIKey)

	msg := failmsg.NoKeyMessage(err)
	assert.Contains(t, msg, "Producer（コンセプト）", "which Agent")
	assert.Contains(t, msg, "openai / gpt-5（API キー未設定）", "which provider and model, first")
	assert.Contains(t, msg, "anthropic / claude-opus-4-5（API キー未設定）", "then the alternative")
	assert.Contains(t, msg, "OpenAI・Anthropic の API キーを登録", "which keys to register")
	assert.Contains(t, msg, "chatgpt-subscription", "the plan sign-in")
	assert.Contains(t, msg, "再起動すると反映されます", "a setting that waits for a restart")
}

func TestNoKeyMessage_WithoutARouteStillPointsAtKeysAndThePlan(t *testing.T) {
	msg := failmsg.NoKeyMessage(errors.Join(llm.ErrNoAPIKey))
	assert.Contains(t, msg, "API キー")
	assert.Contains(t, msg, "chatgpt-subscription")
}

func TestLabel_FallsBackToTheName(t *testing.T) {
	assert.Equal(t, "Writer（脚本）", failmsg.Label(llm.AgentWriter))
	assert.Equal(t, "nobody", failmsg.Label("nobody"))
}

func TestRef_OnlyAMissingKeyLinksToTheLLMSettings(t *testing.T) {
	missing := fmt.Errorf("x: %w", llm.ErrNoAPIKey)
	assert.Equal(t, domain.RefGlobalSettings, failmsg.RefType(missing))
	assert.Equal(t, "llm", failmsg.RefID(missing))
	assert.Empty(t, failmsg.RefType(llm.ErrTimeout))
	assert.Empty(t, failmsg.RefID(llm.ErrTimeout))
}
