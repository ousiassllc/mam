//go:build !windows

package facesim_test

import (
	"context"
	"image"
	_ "image/jpeg" // registers the decoder of the test photos
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// These tests run the real models. They need the two ONNX models, the ONNX Runtime library and
// face photos, none of which is stored in the repository:
//
//	MAM_FACESIM_MODEL_DIR   directory with the two model files (facesim.DetectorFile / RecognizerFile)
//	MAM_FACESIM_RUNTIME_LIB path of libonnxruntime (1.23.x)
//	MAM_FACESIM_PHOTOS      directory with a.jpg (person A), a_small.jpg (person A again,
//	                        different size and brightness), b.jpg (person B), none.jpg (no face)
func engineFromEnv(t *testing.T) (*facesim.ONNX, string) {
	t.Helper()
	dir, photos := os.Getenv("MAM_FACESIM_MODEL_DIR"), os.Getenv("MAM_FACESIM_PHOTOS")
	if dir == "" || photos == "" {
		t.Skip("MAM_FACESIM_MODEL_DIR / MAM_FACESIM_PHOTOS are not set")
	}
	e, err := facesim.NewONNX(facesim.ONNXConfig{ModelDir: dir, RuntimeLibrary: os.Getenv("MAM_FACESIM_RUNTIME_LIB")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	return e, photos
}

func loadPhoto(t *testing.T, dir, name string) image.Image {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, name)) // #nosec G304 -- test fixture path from the environment
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	require.NoError(t, err)
	return img
}

func TestONNX_SamePersonIsCloserThanAnotherPerson(t *testing.T) {
	e, photos := engineFromEnv(t)
	ctx := context.Background()
	faces := func(name string) []facesim.Face {
		fs, err := e.Faces(ctx, loadPhoto(t, photos, name))
		require.NoError(t, err)
		return fs
	}
	a, aAgain, b := faces("a.jpg"), faces("a_small.jpg"), faces("b.jpg")
	require.NotEmpty(t, a)
	require.NotEmpty(t, aAgain)
	require.NotEmpty(t, b)

	same := facesim.Cosine(a[0].Embedding, aAgain[0].Embedding)
	other := facesim.Cosine(a[0].Embedding, b[0].Embedding)
	t.Logf("same=%.4f other=%.4f", same, other)
	assert.Greater(t, same, 0.7, "the same face at another size and brightness")
	assert.Less(t, other, 0.3, "two different people")
	assert.InDelta(t, 1.0, facesim.Cosine(a[0].Embedding, a[0].Embedding), 1e-5)
	assert.False(t, a[0].Box.Empty())
}

func TestONNX_ImageWithoutFaceHasNoFaces(t *testing.T) {
	e, photos := engineFromEnv(t)
	fs, err := e.Faces(context.Background(), loadPhoto(t, photos, "none.jpg"))
	require.NoError(t, err)
	assert.Empty(t, fs)
}

func TestNewONNX_MissingModelsIsUnavailable(t *testing.T) {
	_, err := facesim.NewONNX(facesim.ONNXConfig{ModelDir: t.TempDir()})
	assert.ErrorIs(t, err, facesim.ErrUnavailable)
}
