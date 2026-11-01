package facesim

import (
	"context"
	"image"
	"image/color"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The SFace session only runs when YuNet finds a face, which needs a photo. This runs it on a made-up
// detection of a synthetic image, so that the Windows CI (MAM_FACESIM_MODEL_DIR, MAM_FACESIM_RUNTIME_LIB as in
// onnx_test.go) also proves that the second model creates its tensor, runs and reads back a 128-float embedding.
func TestONNX_EmbedRunsOnTheRealRuntime(t *testing.T) {
	dir := os.Getenv("MAM_FACESIM_MODEL_DIR")
	if dir == "" {
		t.Skip("MAM_FACESIM_MODEL_DIR is not set")
	}
	e, err := NewONNX(ONNXConfig{ModelDir: dir, RuntimeLibrary: os.Getenv("MAM_FACESIM_RUNTIME_LIB")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	img := image.NewRGBA(image.Rect(0, 0, 320, 320))
	for y := range 320 {
		for x := range 320 {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / 319), G: uint8(y * 255 / 319), B: 90, A: 255})
		}
	}
	m := toRGB(img, maxImageSide)
	d := detection{
		x1: 80, y1: 80, x2: 240, y2: 240, score: 0.9,
		landmarks: [5][2]float32{{130, 140}, {190, 140}, {160, 175}, {138, 205}, {182, 205}},
	}
	emb, err := e.embed(context.Background(), m, d, 1)
	require.NoError(t, err)
	assert.Len(t, emb, 128)
	assert.InDelta(t, 1.0, Cosine(emb, emb), 1e-5, "the embedding is L2-normalised")
}
