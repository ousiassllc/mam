package adaptertest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/enhancevideo"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// fakeEnhancer is a video backend that also enhances videos (enhancevideo.Backend).
type fakeEnhancer struct {
	fakeVideo
	got []enhancevideo.Request
}

func (f *fakeEnhancer) EnhanceVideo(_ context.Context, req enhancevideo.Request) (modeladapter.VideoResult, error) {
	f.got = append(f.got, req)
	return modeladapter.VideoResult{File: modeladapter.BlobOf([]byte("enhanced")), MimeType: "video/mp4"}, nil
}

func TestEnhanceVideoCallsTheBackendOfTheModelWithTheRequest(t *testing.T) {
	enh := &fakeEnhancer{}
	b := modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelKling3: enh}}
	a := newAdapter(modeladapter.DefaultConfig(), b)
	req := enhancevideo.Request{Source: modeladapter.Input{Data: []byte("src"), MimeType: "video/mp4"}, Multiplier: 2, OutputFPS: 32}

	res, err := enhancevideo.Run(context.Background(), a, domain.ModelKling3, req)

	require.NoError(t, err)
	assert.Equal(t, "video/mp4", res.MimeType)
	assert.Equal(t, []enhancevideo.Request{req}, enh.got)
	assert.Zero(t, enh.callCount(), "GenerateVideo is not the way in")
}

func TestEnhanceVideoRefusesAModelThatIsNoEnhancementWorkflow(t *testing.T) {
	plain := &fakeVideo{results: []error{nil}}
	b := modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelKling3: plain}}
	a := newAdapter(modeladapter.DefaultConfig(), b)

	_, err := enhancevideo.Run(context.Background(), a, domain.ModelKling3, enhancevideo.Request{})
	assert.ErrorIs(t, err, enhancevideo.ErrNotEnhance)

	_, err = enhancevideo.Run(context.Background(), a, domain.ModelSeedance2, enhancevideo.Request{})
	assert.ErrorIs(t, err, enhancevideo.ErrNotEnhance, "a model without a backend")
	assert.Zero(t, plain.callCount())
}
