package servicetest_test

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// unpricedGPTImage makes gpt-image-2 a cloud model without a price (the user changed its API model ID).
func unpricedGPTImage(c *modeladapter.Config) {
	mc := c.Models[domain.ModelGPTImage2]
	mc.Model, mc.CostPerImage = "gpt-image-1.5", 0
	c.Models[domain.ModelGPTImage2] = mc
}

func TestRunTakesTheConfirmedEstimateOfAModelWithoutAPrice(t *testing.T) {
	for name, models := range map[string][]string{
		"only models without a price": {"gpt-image-2"},
		"mixed with a priced model":   {"flux-2", "gpt-image-2"},
	} {
		t.Run(name, func(t *testing.T) {
			r := gentesttest.New(t, gentesttest.WithConfig(unpricedGPTImage))
			req := imageReq(models...)
			e, err := r.Svc.Estimate(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, []string{"gpt-image-2"}, e.UnknownModels)

			again, err := r.Svc.Estimate(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, e.ID, again.ID, "the same request is the same estimate")

			ref, err := r.Svc.Run(context.Background(), req, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
			require.NoError(t, err, "nothing changed since the confirmation")
			assert.Equal(t, len(models), ref.TotalItems)
			r.Wait(ref.BatchID)
		})
	}
}

func TestRunStopsWhenWhatWasConfirmedChanged(t *testing.T) {
	// reprice reloads the Adapter with the price of gpt-image-2 and flux-2 changed, as the user's save of models.json does.
	reprice := func(r *gentesttest.Rig, gpt, flux float64) {
		cfg, backends := r.Adapter.Snapshot()
		cfg.Models = maps.Clone(cfg.Models)
		for m, price := range map[domain.ModelName]float64{domain.ModelGPTImage2: gpt, domain.ModelFlux2: flux} {
			mc := cfg.Models[m]
			mc.CostPerImage = price
			cfg.Models[m] = mc
		}
		r.Adapter.Reload(cfg, backends)
	}
	for name, change := range map[string]func(r *gentesttest.Rig){
		"a model without a price gets one": func(r *gentesttest.Rig) { reprice(r, 0.05, 0.03) },
		"the price of another model moves": func(r *gentesttest.Rig) { reprice(r, 0, 0.04) },
	} {
		t.Run(name, func(t *testing.T) {
			r := gentesttest.New(t, gentesttest.WithConfig(unpricedGPTImage))
			req := imageReq("flux-2", "gpt-image-2")
			e, err := r.Svc.Estimate(context.Background(), req)
			require.NoError(t, err)

			change(r)
			_, err = r.Svc.Run(context.Background(), req, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
			var c *gterr.ConflictError
			require.ErrorAs(t, err, &c)
			assert.Contains(t, c.Message, "確認した見積り")
			assert.Empty(t, r.Fake.Calls(), "nothing ran")
		})
	}
}

func TestEstimateIsTheSameWhateverLineBreaksThePromptHas(t *testing.T) {
	// A browser sends the text of a textarea with CRLF line breaks, and the hidden field of the confirmation comes back with LF.
	r := gentesttest.New(t)
	typed := imageReq("flux-2")
	typed.Prompts = []gentest.PromptInput{{Prompt: "a fox\r\nin the snow\rat dusk", Negative: "blurry\r\nnoisy"}}
	back := imageReq("flux-2")
	back.Prompts = []gentest.PromptInput{{Prompt: "a fox\nin the snow\nat dusk", Negative: "blurry\nnoisy"}}

	e, err := r.Svc.Estimate(context.Background(), typed)
	require.NoError(t, err)
	assert.Equal(t, typed.Prompts, e.Request.Prompts, "the prompt is kept as typed, not rewritten")
	other, err := r.Svc.Estimate(context.Background(), back)
	require.NoError(t, err)
	assert.Equal(t, e.ID, other.ID)

	ref, err := r.Svc.Run(context.Background(), back, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	require.NoError(t, err)
	r.Wait(ref.BatchID)
	ref, err = r.Svc.Run(context.Background(), typed, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	require.NoError(t, err)
	r.Wait(ref.BatchID)
	var sent []string
	for _, c := range r.Fake.CallsOf("flux-2") {
		sent = append(sent, c.Image.Prompt)
	}
	require.Len(t, sent, 2)
	assert.Contains(t, sent[0]+sent[1], "a fox\nin the snow\nat dusk Avoid: blurry\nnoisy", "the LF prompt is sent as it is")
	assert.Contains(t, sent[0]+sent[1], "a fox\r\nin the snow\rat dusk Avoid: blurry\r\nnoisy", "and the CRLF one, not rewritten")

	changed := imageReq("flux-2")
	changed.Prompts = []gentest.PromptInput{{Prompt: "a fox\nin the snow\nat noon", Negative: "blurry\nnoisy"}}
	_, err = r.Svc.Run(context.Background(), changed, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	var c *gterr.ConflictError
	assert.ErrorAs(t, err, &c, "another text is still another request")
}
