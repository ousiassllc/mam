package voicerefsvc_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// ffprobe handed over at startup may still be missing when the recording comes (it is set up while mam runs, #1047): that is "no ffprobe", not "not audio".
func TestSetForProject_FFprobeThatIsNotFoundYetLeavesTheLengthUnmeasured(t *testing.T) {
	f := newFixture(t)
	f.probe.err = fmt.Errorf("%w: not found", media.ErrUnavailable)

	ref, err := f.svc.SetForProject(context.Background(), "p1", "c1", voicerefsvc.Input{File: file("a.wav", wav(100))})

	require.NoError(t, err)
	assert.Zero(t, ref.DurationMS, "0 = not measured")
}
