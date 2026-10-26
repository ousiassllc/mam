package agegate_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// counting is a backend that answers every call and counts it: a refused call must leave it at zero.
type counting struct{ calls atomic.Int32 }

func (c *counting) GenerateVideo(context.Context, string, modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	c.calls.Add(1)
	return modeladapter.VideoResult{File: modeladapter.BlobOf([]byte("mp4")), MimeType: "video/mp4"}, nil
}

func (c *counting) GenerateImage(context.Context, string, modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	c.calls.Add(1)
	return modeladapter.ImageResult{Data: []byte("png"), MimeType: "image/png"}, nil
}

var (
	seedance = modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "720p"}
	kling    = modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}
	job      = modeladapter.VideoJob{Spec: domain.ShotSpec{ShotObjective: "a walk"}, DurationSeconds: 5}
)

// adapter has a counting backend for every model; the gate refuses refused.
func adapter(refused ...domain.ModelName) (*modeladapter.Adapter, *counting) {
	b := &counting{}
	backends := modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{}, Video: map[domain.ModelName]modeladapter.VideoBackend{}}
	for _, m := range domain.ImageModels() {
		backends.Image[m] = b
	}
	for _, m := range domain.VideoModels() {
		backends.Video[m] = b
	}
	a := modeladapter.New(modeladapter.DefaultConfig(), backends)
	a.Gate.Set(func(m domain.ModelName) bool {
		for _, r := range refused {
			if m == r {
				return true
			}
		}
		return false
	})
	return a, b
}

func TestAGateThatWasNeverSetRefusesNothing(t *testing.T) {
	var g agegate.Gate
	assert.NoError(t, g.Check(domain.ModelSeedance2))
	g.Set(nil)
	assert.NoError(t, g.Check(domain.ModelSeedance2))
}

func TestCheckRefusesTheWholeRouteAndNamesTheFirstRefusedModel(t *testing.T) {
	a, _ := adapter(domain.ModelKling3, domain.ModelRunwayGen45)

	err := a.Gate.Check(domain.ModelSeedance2, domain.ModelKling3, domain.ModelRunwayGen45)

	var refused *agegate.RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, domain.ModelKling3, refused.Model)
	assert.ErrorIs(t, err, agegate.ErrRefused)
	assert.Equal(t, err.Error(), agegate.Reason(fmt.Errorf("every video model failed: %w", err)), "the reason is the sentence of the refusal, however it is wrapped")
	assert.Empty(t, agegate.Reason(context.Canceled))
}

// A refused model of a video route is refused whole, before any call: the alternative does not make the Take that the
// primary was refused for, and a refused alternative is refused although the primary would have worked.
func TestGenerateVideoRefusesARouteThatNamesARefusedModelWithoutCallingAnyBackend(t *testing.T) {
	for name, c := range map[string]struct{ primary, fallback modeladapter.VideoTarget }{
		"the primary is refused":     {seedance, kling},
		"the alternative is refused": {kling, seedance},
		"both are refused":           {seedance, seedance},
	} {
		t.Run(name, func(t *testing.T) {
			a, backend := adapter(domain.ModelSeedance2)

			_, err := a.GenerateVideo(context.Background(), c.primary, c.fallback, job)

			require.ErrorIs(t, err, agegate.ErrRefused)
			assert.Zero(t, backend.calls.Load(), "no model was called, the alternative neither")
		})
	}
	a, backend := adapter()
	out, err := a.GenerateVideo(context.Background(), seedance, kling, job)
	require.NoError(t, err)
	assert.Equal(t, domain.ModelSeedance2, out.Model, "with nothing refused the primary makes it")
	assert.EqualValues(t, 1, backend.calls.Load())
}

func TestGenerateImageRefusesARouteThatNamesARefusedModelWithoutCallingAnyBackend(t *testing.T) {
	route := modeladapter.DefaultConfig().ImageRouteFor(modeladapter.PurposeNewReference)
	for name, refused := range map[string]domain.ModelName{"primary": route.Primary.Model, "alternative": route.Fallback.Model} {
		t.Run(name, func(t *testing.T) {
			a, backend := adapter(refused)

			_, err := a.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "p"})

			require.ErrorIs(t, err, agegate.ErrRefused)
			assert.Zero(t, backend.calls.Load())
		})
	}
}

// The direct calls of the generation test are gated too, so a request that names a refused model never reaches it.
func TestDirectCallsRefuseARefusedModel(t *testing.T) {
	a, backend := adapter(domain.ModelSeedance2, domain.ModelNanoBanana)

	_, err := direct.Image(context.Background(), a, domain.ModelNanoBanana, modeladapter.ImageRequest{Prompt: "p"})
	require.ErrorIs(t, err, agegate.ErrRefused)
	_, err = direct.Video(context.Background(), a, seedance, modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5})
	require.ErrorIs(t, err, agegate.ErrRefused)
	assert.Zero(t, backend.calls.Load())

	_, err = direct.Video(context.Background(), a, kling, modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5})
	require.NoError(t, err, "a model that is not refused is called")
}

// Multi-model routing and the generation test ask Unavailable: a refused model is left out with the reason of the gate.
func TestARefusedModelIsUnavailable(t *testing.T) {
	a, _ := adapter(domain.ModelSeedance2)

	assert.Equal(t, agegate.Excluded, a.Unavailable(domain.ModelSeedance2))
	assert.Empty(t, a.Unavailable(domain.ModelKling3))
}
