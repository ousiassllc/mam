package servicetest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

func p(s string) gentest.PromptInput { return gentest.PromptInput{Prompt: s} }

func imageReq(models ...string) gentest.Request {
	return gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{p("a fox")}}
}

func line(t *testing.T, e gentest.Estimate, model string) gentest.ModelEstimate {
	t.Helper()
	for _, l := range e.Breakdown {
		if l.Model == model {
			return l
		}
	}
	require.Failf(t, "no line", "%s in %+v", model, e.Breakdown)
	return gentest.ModelEstimate{}
}

func TestEstimateMultipliesModelsPromptsAndCountAndPricesEachModel(t *testing.T) {
	r := gentesttest.New(t)
	req := imageReq("flux-2", "nano-banana")
	req.Prompts = append(req.Prompts, p("a cat"), gentest.PromptInput{Prompt: "a fox"}) // the repeated prompt counts once
	req.Count = 3

	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, 2*2*3, e.TotalItems)
	assert.InDelta(t, 6*0.03+6*0.0336, e.TotalUSD, 1e-9)
	flux := line(t, e, "flux-2")
	assert.Equal(t, 6, flux.Items)
	assert.Equal(t, "priced", flux.Basis)
	require.NotNil(t, flux.SubtotalUSD)
	assert.InDelta(t, 0.18, *flux.SubtotalUSD, 1e-9)
	assert.Equal(t, 50, e.MaxItems)
	assert.InDelta(t, 20.0, e.MaxCostUSD, 1e-9)
	assert.Empty(t, e.OverLimit)
	assert.NotEmpty(t, e.ID)
	var seeds int
	for _, w := range e.Warnings {
		if w.Code == gentest.WarnSeedUnsupported {
			seeds++
			assert.Equal(t, "nano-banana", w.Model, "only the model without a seed field is warned")
		}
	}
	assert.Equal(t, 1, seeds)
}

func TestEstimateVideoUsesTheSnappedLengthAndTheResolutionPrice(t *testing.T) {
	r := gentesttest.New(t)
	e, err := r.Svc.Estimate(context.Background(), gentest.Request{
		Media: "video", Models: []string{"kling-3.0", "runway-gen-4.5", "seedance-2.0"}, Prompts: []gentest.PromptInput{p("waves")},
		Resolution: "720p", DurationSeconds: 6, Count: 2,
	})
	require.NoError(t, err)

	kling := line(t, e, "kling-3.0")
	assert.Equal(t, 10.0, kling.Seconds, "Kling makes 5 or 10 seconds: 6 snaps up to 10")
	assert.InDelta(t, 0.07*10*2, *kling.SubtotalUSD, 1e-9)
	runway := line(t, e, "runway-gen-4.5")
	assert.Equal(t, 6.0, runway.Seconds)
	assert.InDelta(t, 0.12*6*2, *runway.SubtotalUSD, 1e-9)
	assert.InDelta(t, 0.08*6*2, *line(t, e, "seedance-2.0").SubtotalUSD, 1e-9)
	var snapped []string
	for _, w := range e.Warnings {
		if w.Code == gentest.WarnDurationSnapped {
			snapped = append(snapped, w.Model)
		}
	}
	assert.Equal(t, []string{"kling-3.0"}, snapped)
}

func TestEstimateOfAnImageFollowsItsSize(t *testing.T) {
	r := gentesttest.New(t)
	landscape, square := imageReq("gpt-image-2"), imageReq("gpt-image-2")
	landscape.AspectRatio, square.AspectRatio = "16:9", "1:1"

	l, err := r.Svc.Estimate(context.Background(), landscape)
	require.NoError(t, err)
	q, err := r.Svc.Estimate(context.Background(), square)
	require.NoError(t, err)

	assert.Equal(t, "priced", line(t, l, "gpt-image-2").Basis)
	assert.InDelta(t, 0.041, *line(t, l, "gpt-image-2").UnitUSD, 1e-9, "medium at 1536x1024")
	assert.InDelta(t, 0.053, *line(t, q, "gpt-image-2").UnitUSD, 1e-9, "medium at 1024x1024")
}

func TestEstimateIsTriState(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		mc := c.Models[domain.ModelGPTImage2]
		mc.Model, mc.CostPerImage = "gpt-image-1.5", 0 // a cloud model the built-in prices do not cover (the user changed its API model ID)
		c.Models[domain.ModelGPTImage2] = mc
	}))
	e, err := r.Svc.Estimate(context.Background(), imageReq("flux-2", "gpt-image-2"))
	require.NoError(t, err)

	assert.Equal(t, "priced", line(t, e, "flux-2").Basis)
	unknown := line(t, e, "gpt-image-2")
	assert.Equal(t, "unknown", unknown.Basis)
	assert.Nil(t, unknown.UnitUSD)
	assert.Nil(t, unknown.SubtotalUSD)
	assert.InDelta(t, 0.03, e.TotalUSD, 1e-9, "an unknown price is left out of the total")
	assert.Equal(t, 1, e.UnknownItems)
	assert.Equal(t, []string{"gpt-image-2"}, e.UnknownModels)
	assert.Contains(t, codes(e), gentest.WarnNoPrice)
}

func codes(e gentest.Estimate) []string {
	out := []string{}
	for _, w := range e.Warnings {
		out = append(out, w.Code)
	}
	return out
}

func TestEstimateWarnsAboutInputsAModelCannotUse(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithNoReferenceModel())
	id, _, err := r.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	req := imageReq(string(gentesttest.NoReferenceModel), "flux-2")
	req.References = []gentest.RefInput{{Source: gentest.FromStaged, ID: id}}

	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	var dropped []string
	for _, w := range e.Warnings {
		if w.Code == gentest.WarnInputsDropped {
			dropped = append(dropped, w.Model)
		}
	}
	assert.Equal(t, []string{string(gentesttest.NoReferenceModel)}, dropped, "a local model without image inputs throws reference images away")

	vreq := gentest.Request{Media: "video", Models: []string{"runway-gen-4.5", "kling-3.0", "seedance-2.0"}, Prompts: []gentest.PromptInput{p("x")}, AspectRatio: "1:1",
		References: []gentest.RefInput{{Source: gentest.FromStaged, ID: id, Role: "reference"}, {Source: gentest.FromStaged, ID: id, Role: "last_frame"}}}
	e, err = r.Svc.Estimate(context.Background(), vreq)
	require.NoError(t, err)
	byModel := map[string][]string{}
	for _, w := range e.Warnings {
		byModel[w.Model] = append(byModel[w.Model], w.Code)
	}
	assert.Contains(t, byModel["runway-gen-4.5"], gentest.WarnAspect, "Runway 1:1 needs a first frame")
	assert.Contains(t, byModel["runway-gen-4.5"], gentest.WarnInputsDropped, "no free references and no last frame")
	assert.Contains(t, byModel["kling-3.0"], gentest.WarnInputsDropped)
	assert.Equal(t, []string{gentest.WarnInputsDropped}, byModel["seedance-2.0"], "Seedance takes the free reference; only the last frame (it has no first frame) is dropped")
}

func TestEstimateFoldsTheNegativePromptWhereThereIsNoField(t *testing.T) {
	r := gentesttest.New(t)
	req := gentest.Request{Media: "video", Models: []string{"kling-3.0", "seedance-2.0"}, Prompts: []gentest.PromptInput{{Prompt: "waves", Negative: "text"}}}
	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	var folded []string
	for _, w := range e.Warnings {
		if w.Code == gentest.WarnNegativeFolded {
			folded = append(folded, w.Model)
		}
	}
	assert.Equal(t, []string{"seedance-2.0"}, folded)
}

func TestEstimateLimitsAreEnforcedAndConfigurable(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		items, cost := 5, 0.10
		c.Gentest.MaxItems, c.Gentest.MaxCostUSD = &items, &cost
	}))
	req := imageReq("flux-2", "nano-banana")
	req.Count = 3
	_, err := r.Svc.Estimate(context.Background(), req)
	var invalid *gterr.InvalidError
	require.ErrorAs(t, err, &invalid, "6 results are over max_items 5")
	assert.Contains(t, invalid.Message, "5")

	req.Count = 2 // 4 results: 0.03*2 + 0.0336*2 = 0.1272 > 0.10
	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	assert.NotEmpty(t, e.OverLimit, "the screen shows the figures, and the confirmation is refused")
	_, err = r.Svc.Run(context.Background(), req, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	var c *gterr.ConflictError
	assert.ErrorAs(t, err, &c)

	zero := 0.0
	r2 := gentesttest.New(t, gentesttest.WithConfig(func(c *modeladapter.Config) { c.Gentest.MaxCostUSD = &zero }))
	big := imageReq("gpt-image-2")
	big.Count = 20
	e, err = r2.Svc.Estimate(context.Background(), big)
	require.NoError(t, err)
	assert.Empty(t, e.OverLimit, "0 is no limit")
}

func TestEstimateRefusesWhatCannotRun(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithoutBackend(domain.ModelFlux2))
	for name, req := range map[string]gentest.Request{
		"no media":      {Models: []string{"flux-2"}, Prompts: []gentest.PromptInput{p("x")}},
		"no model":      {Media: "image", Prompts: []gentest.PromptInput{p("x")}},
		"no prompt":     {Media: "image", Models: []string{"nano-banana"}},
		"blank prompt":  {Media: "image", Models: []string{"nano-banana"}, Prompts: []gentest.PromptInput{p("  ")}},
		"unknown model": {Media: "image", Models: []string{"nope"}, Prompts: []gentest.PromptInput{p("x")}},
		"video model":   {Media: "image", Models: []string{"kling-3.0"}, Prompts: []gentest.PromptInput{p("x")}},
		"no backend":    {Media: "image", Models: []string{"flux-2"}, Prompts: []gentest.PromptInput{p("x")}},
		"twice":         {Media: "image", Models: []string{"nano-banana", "nano-banana"}, Prompts: []gentest.PromptInput{p("x")}},
		"bad aspect":    {Media: "image", Models: []string{"nano-banana"}, Prompts: []gentest.PromptInput{p("x")}, AspectRatio: "wide"},
		"count":         {Media: "image", Models: []string{"nano-banana"}, Prompts: []gentest.PromptInput{p("x")}, Count: 21},
		"duration":      {Media: "image", Models: []string{"nano-banana"}, Prompts: []gentest.PromptInput{p("x")}, DurationSeconds: 5},
		"frame on img":  {Media: "image", Models: []string{"nano-banana"}, Prompts: []gentest.PromptInput{p("x")}, References: []gentest.RefInput{{Source: gentest.FromStaged, ID: "x", Role: "first_frame"}}},
	} {
		_, err := r.Svc.Estimate(context.Background(), req)
		var invalid *gterr.InvalidError
		assert.ErrorAs(t, err, &invalid, name)
	}
}

func TestModelsListsEveryBuiltInModelAndWhyOneCannotBeChosen(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithNoReferenceModel(), gentesttest.WithoutBackend(domain.ModelNanoBananaLite), gentesttest.WithoutBackend(domain.ModelSeedance2))
	byID := map[string]modelcatalog.Info{}
	for _, m := range r.Svc.Models(context.Background(), "") {
		byID[m.ID] = m
	}
	for _, id := range []string{"nano-banana-lite", "nano-banana", "flux-2", "gpt-image-2", "kling-3.0", "seedance-2.0", "runway-gen-4.5", "gemini-omni-flash", "kling-4.0", "seedance-2.5"} {
		assert.Contains(t, byID, id)
	}
	assert.False(t, byID["nano-banana-lite"].Available)
	assert.Equal(t, domain.ExcludedNoBackend, byID["nano-banana-lite"].Unavailable)
	assert.NotEmpty(t, byID["nano-banana-lite"].UnavailableMessage, "the user is told why")
	assert.False(t, byID["seedance-2.0"].Available)
	assert.True(t, byID["flux-2"].Available)
	assert.True(t, byID["flux-2"].SupportsSeed)
	assert.False(t, byID["nano-banana"].SupportsSeed)
	assert.True(t, byID["kling-3.0"].NegativeField)
	assert.True(t, byID["gpt-image-2"].AcceptsReferences)
	assert.True(t, byID["nano-banana-lite"].AcceptsReferences)
	assert.False(t, byID[string(gentesttest.NoReferenceModel)].AcceptsReferences, "a local model can be without image inputs")
	require.NotNil(t, byID["runway-gen-4.5"].Video)
	assert.False(t, byID["runway-gen-4.5"].Video.LastFrame)
	assert.Equal(t, "priced", byID["kling-3.0"].Price.Basis)
	assert.InDelta(t, 0.14, byID["kling-3.0"].Price.PerSecondUSD["1080p"], 1e-9)

	assert.Len(t, r.Svc.Models(context.Background(), "video"), 6)
	_, err := r.Svc.Model(context.Background(), "image", "kling-3.0")
	assert.ErrorIs(t, err, gterr.ErrNotFound)
}

func TestRunNeedsTheConfirmedEstimate(t *testing.T) {
	r := gentesttest.New(t)
	req := imageReq("flux-2")
	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)

	cases := map[string]gentest.Confirmation{
		"none":         {},
		"wrong id":     {EstimateID: "x", TotalUSD: e.TotalUSD},
		"wrong amount": {EstimateID: e.ID, TotalUSD: e.TotalUSD + 1},
	}
	for name, c := range cases {
		_, err := r.Svc.Run(context.Background(), req, c)
		require.Error(t, err, name)
	}
	assert.Empty(t, r.Fake.Calls(), "nothing is generated without the confirmation")

	// The same confirmation does not carry over to another request, or to prices that changed.
	other := imageReq("flux-2", "nano-banana")
	_, err = r.Svc.Run(context.Background(), other, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	var c *gterr.ConflictError
	assert.ErrorAs(t, err, &c)
	assert.Contains(t, c.Message, "確認した見積り")

	ref, err := r.Svc.Run(context.Background(), req, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	require.NoError(t, err)
	assert.Equal(t, 1, ref.TotalItems)
	assert.NotEmpty(t, ref.JobID)
	r.Wait(ref.BatchID)
}
