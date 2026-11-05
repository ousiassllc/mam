package media

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbe_CountsTheFramesOfTheVideoStream(t *testing.T) {
	tools := lookOrSkip(t)
	path := makeVideo(t, tools, "frames.mp4", 64, 64, 10, 2, false)

	info, err := tools.Probe(context.Background(), path)

	require.NoError(t, err)
	assert.Equal(t, 20, info.FrameCount)
}
