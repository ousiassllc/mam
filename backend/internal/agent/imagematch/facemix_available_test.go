package imagematch_test

import (
	"context"
	"image"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// lateFaces is an engine whose models are installed while mam runs (#1026): it is off until on is set.
type lateFaces struct {
	inner *fakeFaces
	on    atomic.Bool
}

func (e *lateFaces) Faces(ctx context.Context, img image.Image) ([]facesim.Face, error) {
	if !e.on.Load() {
		return nil, facesim.ErrUnavailable
	}
	return e.inner.Faces(ctx, img)
}

func (e *lateFaces) Available() bool { return e.on.Load() }

// While the engine is off the round's score is the LLM's alone and says "unavailable" (the engine is not even asked); once the models are installed the same Matcher mixes the face in.
func TestCompareMixesTheFaceInOnceTheModelsAreInstalledWhileTheAppRuns(t *testing.T) {
	late := &lateFaces{inner: &fakeFaces{}}
	m := mixed(0.3, late)

	off := compare(t, m, personA, personB)
	assert.Equal(t, 60, off.Score)
	require.NotNil(t, off.Scoring)
	assert.Equal(t, imagematch.SkipUnavailable, off.Scoring.Skipped)
	assert.Zero(t, late.inner.calls)

	late.on.Store(true)

	on := compare(t, m, personA, personB)
	assert.Equal(t, 66, on.Score)
	require.NotNil(t, on.Scoring)
	require.NotNil(t, on.Scoring.Face)
}
