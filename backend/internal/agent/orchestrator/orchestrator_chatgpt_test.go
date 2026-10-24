package orchestrator_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestHandle_AFallbackBecauseOfAUsageLimitIsToldInTheChatOnceAndTheRunContinues(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	const reason = "ChatGPT のプランの利用上限に達しました。利用状況: https://chatgpt.com/settings/usage"
	// A real llm.Client reports through the context llm.WithFallbackNotice put there: drive the same hook through it.
	f.orch = orchestrator.New(orchestrator.Deps{
		Projects: f.projects, Plans: f.plans, ChatRepo: f.chatRepo, Chat: f.chat, Jobs: f.jobs, Notes: f.notes,
		LLM: usageLimitClient{f.llm, reason},
	})
	id := f.project(t, seed{brief: completeBrief()})

	require.NoError(t, f.handle(context.Background(), id))

	texts := f.chat.texts()
	var told int
	for _, text := range texts {
		if assert.ObjectsAreEqual(reason+" 代替のプロバイダーに切り替えて続けます。", text) {
			told++
		}
	}
	assert.Equal(t, 1, told, "the reason is told once although the pipeline made several calls: %v", texts)
	assert.Equal(t, domain.StateVisualDesign, f.state(t, id), "the run was not stopped by the fallback")
}

// usageLimitClient routes every call through a real llm.Client whose primary fails with a usage limit.
type usageLimitClient struct {
	next   llm.Completer
	reason string
}

func (u usageLimitClient) Complete(ctx context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	cfg := llm.DefaultConfig()
	cfg.Routes[agent] = llm.Route{
		Primary:  llm.Target{Provider: llm.ProviderChatGPTSubscription, Model: "m"},
		Fallback: llm.Target{Provider: llm.ProviderOpenAI, Model: "m"},
	}
	client := llm.NewClient(cfg, map[llm.Provider]llm.Backend{
		llm.ProviderChatGPTSubscription: failingBackend{u.reason},
		llm.ProviderOpenAI:              answerBackend{u.next, agent},
	})
	return client.Complete(ctx, agent, req)
}

type failingBackend struct{ reason string }

func (b failingBackend) Complete(context.Context, string, llm.Request) (string, error) {
	return "", &llm.APIError{Provider: llm.ProviderChatGPTSubscription, Status: 429, Permanent: true, Notice: b.reason}
}

type answerBackend struct {
	next  llm.Completer
	agent llm.Agent
}

func (b answerBackend) Complete(ctx context.Context, _ string, req llm.Request) (string, error) {
	res, err := b.next.Complete(ctx, b.agent, req)
	return res.Text, err
}

func TestHandle_WhenEveryProviderFailedTheChatShowsTheCauseTheProviderGave(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})
	const reason = "ChatGPT のログインの有効期限が切れました。設定画面でログインし直してください。"
	f.llm.errs[llm.AgentProducer] = &llm.APIError{Provider: llm.ProviderChatGPTSubscription, Permanent: true, Notice: reason}

	err := f.handle(context.Background(), id)

	require.Error(t, err)
	assert.Contains(t, err.Error(), reason)
	texts := f.chat.texts()
	require.Len(t, texts, 1)
	assert.Contains(t, texts[0], reason)
}
