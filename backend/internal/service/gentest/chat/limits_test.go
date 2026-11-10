package chat_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

func TestAConfirmationOfAnEstimateThatChangedDoesNotRun(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.LLM.queue(estimateStep(1, "flux-2"))
	rep, err := r.Chat.Send(ctx, r.SessID, "1 枚")
	require.NoError(t, err)
	p := proposalOf(t, rep)

	// the price of the model changes between the card and the press (a new models.json)
	cfg, backends := r.Adapter.Snapshot()
	mc := cfg.Models[domain.ModelFlux2]
	mc.CostPerImage *= 2
	cfg.Models[domain.ModelFlux2] = mc
	r.Adapter.Reload(cfg, backends)

	rep, err = r.Chat.Confirm(ctx, r.SessID, p.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StateStale, rep.Changed[0].State)
	assert.Contains(t, rep.Changed[0].Reason, "確認した見積りと今の見積りが違います")
	assert.Equal(t, []chat.Kind{chat.KindStatus}, kinds(rep.Messages))
	assert.True(t, rep.Messages[0].Failed)
	assert.Equal(t, 0, r.batches(t), "nothing was generated")
	assert.Empty(t, r.Fake.Calls())

	_, err = r.Chat.Confirm(ctx, r.SessID, p.ID)
	assert.ErrorIs(t, err, chat.ErrProposalClosed, "a stale card stays closed: estimate again")
}

func TestAConfirmationOverTheCostLimitDoesNotRun(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.LLM.queue(estimateStep(2, "flux-2", "nano-banana"))
	rep, err := r.Chat.Send(ctx, r.SessID, "2 枚ずつ")
	require.NoError(t, err)
	p := proposalOf(t, rep)

	// the limit is lowered below the total that was shown
	cfg, backends := r.Adapter.Snapshot()
	limit := 0.10
	cfg.Gentest.MaxCostUSD = &limit
	r.Adapter.Reload(cfg, backends)

	rep, err = r.Chat.Confirm(ctx, r.SessID, p.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StateStale, rep.Changed[0].State)
	assert.Contains(t, rep.Changed[0].Reason, "上限")
	assert.Equal(t, 0, r.batches(t))
	assert.Empty(t, r.Fake.Calls())
}

func TestAnEstimateOverTheLimitsGivesNoCardAndTheLLMFixesIt(t *testing.T) {
	r := newRig(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		items, cost := 5, 0.10
		c.Gentest.MaxItems, c.Gentest.MaxCostUSD = &items, &cost
	}))
	ctx := context.Background()
	r.LLM.queue(
		estimateStep(3, "flux-2", "nano-banana"), // 6 results: over max_items 5
		estimateStep(2, "flux-2", "nano-banana"), // 4 results, 0.1272 USD: over max_cost_usd 0.10
		estimateStep(1, "flux-2", "nano-banana"), // 2 results, 0.0636 USD: fits
	)
	rep, err := r.Chat.Send(ctx, r.SessID, "たくさん作って")
	require.NoError(t, err)
	assert.Equal(t, []chat.Kind{chat.KindUser, chat.KindStatus, chat.KindStatus, chat.KindAssistant}, kinds(rep.Messages))
	assert.Contains(t, rep.Messages[1].Text, "5", "the line says the limit of results")
	assert.Contains(t, rep.Messages[2].Text, "0.1000", "the line says the limit of the cost")
	p := proposalOf(t, rep)
	assert.Equal(t, 2, p.Estimate.TotalItems)

	// the LLM read why each was refused, and was told the limits from the start
	asked := r.LLM.asked()
	require.Len(t, asked, 3)
	assert.Contains(t, asked[1].User, "estimate refused: ")
	assert.Contains(t, asked[2].User, "estimate refused: ")
	assert.Contains(t, asked[0].System, "5 results and 0.10 USD")
	assert.Equal(t, 0, r.batches(t))
}

func TestAnEstimateTheLLMKeepsOverTheLimitsEndsWithoutACard(t *testing.T) {
	r := newRig(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		items := 1
		c.Gentest.MaxItems = &items
	}))
	steps := make([]string, chat.MaxSteps)
	for i := range steps {
		steps[i] = estimateStep(1, "flux-2", "nano-banana")
	}
	r.LLM.queue(steps...)
	rep, err := r.Chat.Send(context.Background(), r.SessID, "2 モデルで")
	require.NoError(t, err)
	last := rep.Messages[len(rep.Messages)-1]
	assert.Equal(t, chat.KindAssistant, last.Kind)
	assert.Nil(t, last.Proposal, "no card for a batch that cannot run")
	assert.Equal(t, chat.MaxSteps, len(r.LLM.asked()), "the turn is bounded")
}
