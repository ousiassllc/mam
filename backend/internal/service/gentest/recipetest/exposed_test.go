// Package recipetest pins the recipes of the generation test and the values of the exposed inputs (Issue #977): from the request to the call the model got
// and to what the result keeps, over the fake backends of gentesttest.
package recipetest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

var ctx = context.Background

func ptr[T any](v T) *T { return &v }

var exposedModel = string(gentesttest.ExposedModel)

func exposedRig(t *testing.T) *gentesttest.Rig {
	t.Helper()
	return gentesttest.New(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel())
}

func request(models ...string) gentest.Request {
	return gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}}
}

func TestTheModelListSaysWhichInputsAreExposedWithTheValueTheGraphHolds(t *testing.T) {
	r := exposedRig(t)
	m, err := r.Svc.Model(ctx(), gentest.MediaImage, exposedModel)
	require.NoError(t, err)
	require.Len(t, m.Exposed, 3)
	byKey := map[string]any{}
	for _, c := range m.Exposed {
		byKey[c.Key] = c.Default
	}
	assert.Equal(t, "mam/image", byKey[gentesttest.ExposedPrefix])
	assert.EqualValues(t, 1, byKey[gentesttest.ExposedBatch])
	assert.InDelta(t, 1.0, byKey[gentesttest.ExposedDenoise], 0)
	other, err := r.Svc.Model(ctx(), gentest.MediaImage, string(gentesttest.SamplingModel))
	require.NoError(t, err)
	assert.Empty(t, other.Exposed, "a model without exposed inputs lists none")
}

// The value reaches the model's call, the result keeps every value the model ran with (the graph's own where the request named none), and a model that
// does not have the input is told so and runs as it was.
func TestAnExposedValueReachesTheCallAndTheResultKeepsIt(t *testing.T) {
	r := exposedRig(t)
	req := request(exposedModel, string(gentesttest.SamplingModel))
	req.Exposed = map[string]any{gentesttest.ExposedDenoise: "0.6", gentesttest.ExposedBatch: 2}
	est, err := r.Svc.Estimate(ctx(), req)
	require.NoError(t, err)
	var codes []string
	for _, w := range est.Warnings {
		if w.Code == gentest.WarnExposedIgnored {
			assert.Equal(t, string(gentesttest.SamplingModel), w.Model)
			codes = append(codes, w.Code)
		}
	}
	assert.Len(t, codes, 1, "the model that has no such input says so")

	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State)
	for _, res := range st.Results {
		switch res.Model {
		case exposedModel:
			assert.Equal(t, map[string]any{gentesttest.ExposedDenoise: 0.6, gentesttest.ExposedBatch: float64(2), gentesttest.ExposedPrefix: "mam/image"}, res.Exposed)
		default:
			assert.Empty(t, res.Exposed)
		}
	}
	calls := r.Fake.CallsOf(gentesttest.ExposedModel)
	require.Len(t, calls, 1)
	assert.InDelta(t, 0.6, calls[0].Image.Options.Exposed[gentesttest.ExposedDenoise], 0)
	assert.Empty(t, r.Fake.CallsOf(gentesttest.SamplingModel)[0].Image.Options.Exposed)
}

func TestAnExposedValueThatDoesNotFitIsRefused(t *testing.T) {
	r := exposedRig(t)
	for name, values := range map[string]map[string]any{
		"above the range":       {gentesttest.ExposedDenoise: 1.5},
		"not a whole number":    {gentesttest.ExposedBatch: 2.5},
		"not a number":          {gentesttest.ExposedBatch: "many"},
		"an input that is none": {"3.steps": 30},
	} {
		req := request(exposedModel)
		req.Exposed = values
		_, err := r.Svc.Estimate(ctx(), req)
		var invalid *gterr.InvalidError
		assert.ErrorAs(t, err, &invalid, name)
	}
}

// Nothing named: the model runs with the values its graph holds (sent as they are, so that the result can be made again), and the result says which.
func TestWithoutExposedValuesTheGraphValuesAreKept(t *testing.T) {
	r := exposedRig(t)
	ref, err := r.Confirmed(request(exposedModel))
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State)
	assert.InDelta(t, 1.0, st.Results[0].Exposed[gentesttest.ExposedDenoise], 0)
	assert.Equal(t, st.Results[0].Exposed, r.Fake.CallsOf(gentesttest.ExposedModel)[0].Image.Options.Exposed)
}
