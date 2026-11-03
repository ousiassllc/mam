package facesim_test

import (
	"bytes"
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// With the real models (the variables of onnx_test.go), the cut of a photo is the face of the photo: it has a face, it is the same
// person as the photo, and it is smaller than the photo.
func TestONNX_CropFaceKeepsTheFace(t *testing.T) {
	e, photos := engineFromEnv(t)
	data, err := os.ReadFile(filepath.Join(photos, "a.jpg")) //nolint:gosec // test fixture path from the environment
	require.NoError(t, err)
	whole, _, err := image.Decode(bytes.NewReader(data))
	require.NoError(t, err)

	cut, ok, err := facesim.CropFace(context.Background(), e, data)
	require.NoError(t, err)
	if !ok {
		t.Skip("the photo is a close-up already: nothing to cut")
	}
	cropped, _, err := image.Decode(bytes.NewReader(cut))
	require.NoError(t, err)
	assert.Less(t, cropped.Bounds().Dx(), whole.Bounds().Dx())

	same, missing, err := facesim.Similarity(context.Background(), e, whole, cropped)
	require.NoError(t, err)
	require.Equal(t, facesim.BothHaveFaces, missing, "the cut has a face")
	t.Logf("photo %v, cut %v, similarity %.4f", whole.Bounds().Size(), cropped.Bounds().Size(), same)
	assert.Greater(t, same, 0.7, "the cut is the same person")
}
