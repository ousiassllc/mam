package facesim_test

import (
	"context"
	"image"
	"image/color"
	_ "image/jpeg" // registers the decoder of the test photos
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// These tests run the real models. They need the two ONNX models and the ONNX Runtime library, and the
// photo tests face photos, none of which is stored in the repository:
//
//	MAM_FACESIM_MODEL_DIR   directory with the two model files (facesim.DetectorFile / RecognizerFile;
//	                        scripts/fetch-face-models.sh)
//	MAM_FACESIM_RUNTIME_LIB path of the ONNX Runtime library (1.23.x; scripts/fetch-onnxruntime.sh puts
//	                        the Windows DLL into a directory). Empty: the system's library search
//	MAM_FACESIM_PHOTOS      directory with a.jpg (person A), a_small.jpg (person A again,
//	                        different size and brightness), b.jpg (person B), none.jpg (no face)
//
// ci.yml's windows job runs the ones that need no photos (TestONNX_Synthetic*, TestONNX_NonASCII*, and
// TestONNX_Embed* in onnx_internal_test.go) with the real onnxruntime.dll, which proves that the Windows binding
// loads the DLL, creates sessions and runs both models.
func modelDirFromEnv(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("MAM_FACESIM_MODEL_DIR")
	if dir == "" {
		t.Skip("MAM_FACESIM_MODEL_DIR is not set")
	}
	return dir
}

// newEngine loads the models of modelDir on the library of MAM_FACESIM_RUNTIME_LIB, or on lib when one is given.
func newEngine(t *testing.T, modelDir string, lib ...string) *facesim.ONNX {
	t.Helper()
	runtimeLib := os.Getenv("MAM_FACESIM_RUNTIME_LIB")
	if len(lib) > 0 {
		runtimeLib = lib[0]
	}
	e, err := facesim.NewONNX(facesim.ONNXConfig{ModelDir: modelDir, RuntimeLibrary: runtimeLib})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func engineFromEnv(t *testing.T) (*facesim.ONNX, string) {
	t.Helper()
	dir, photos := modelDirFromEnv(t), os.Getenv("MAM_FACESIM_PHOTOS")
	if photos == "" {
		t.Skip("MAM_FACESIM_PHOTOS is not set")
	}
	return newEngine(t, dir), photos
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

// syntheticImage is a smooth colour gradient: it has no face, but runs the whole pipeline (resize, YuNet).
func syntheticImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := range 240 {
		for x := range 320 {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / 319), G: uint8(y * 255 / 239), B: 128, A: 255})
		}
	}
	return img
}

// The models and the runtime library need no photo to be exercised: this loads the library, creates both
// sessions, builds a tensor, runs the detector and reads its outputs back.
func TestONNX_SyntheticImageRunsOnTheRealRuntime(t *testing.T) {
	e := newEngine(t, modelDirFromEnv(t))
	faces, err := e.Faces(context.Background(), syntheticImage())
	require.NoError(t, err)
	assert.Empty(t, faces)
}

// ONNX Runtime takes the model path as UTF-16 on Windows: a path with non-ASCII characters (a user name with
// them is common in %AppData%) must load.
func TestONNX_NonASCIIModelPathLoads(t *testing.T) {
	src := modelDirFromEnv(t)
	dir := filepath.Join(t.TempDir(), "顔モデル")
	require.NoError(t, os.Mkdir(dir, 0o750))
	for _, name := range []string{facesim.DetectorFile, facesim.RecognizerFile} {
		data, err := os.ReadFile(filepath.Join(src, name)) // #nosec G304 -- test fixture path from the environment
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	}
	e := newEngine(t, dir)
	_, err := e.Faces(context.Background(), syntheticImage())
	require.NoError(t, err)
}

func TestNewONNX_MissingRuntimeLibraryIsUnavailable(t *testing.T) {
	models, lib := t.TempDir(), filepath.Join(t.TempDir(), "nope", facesim.WindowsRuntimeLibraryFile)
	for _, name := range []string{facesim.DetectorFile, facesim.RecognizerFile} {
		require.NoError(t, os.WriteFile(filepath.Join(models, name), []byte("not a model"), 0o600))
	}
	_, err := facesim.NewONNX(facesim.ONNXConfig{ModelDir: models, RuntimeLibrary: lib})
	require.ErrorIs(t, err, facesim.ErrUnavailable)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Contains(t, err.Error(), lib, "the reason names the file that is missing")
}

func TestNewONNX_FileThatIsNotALibraryIsUnavailable(t *testing.T) {
	models := t.TempDir()
	for _, name := range []string{facesim.DetectorFile, facesim.RecognizerFile} {
		require.NoError(t, os.WriteFile(filepath.Join(models, name), []byte("not a model"), 0o600))
	}
	lib := filepath.Join(t.TempDir(), facesim.WindowsRuntimeLibraryFile)
	require.NoError(t, os.WriteFile(lib, []byte("not a library"), 0o600))
	_, err := facesim.NewONNX(facesim.ONNXConfig{ModelDir: models, RuntimeLibrary: lib})
	require.ErrorIs(t, err, facesim.ErrUnavailable)
	if runtime.GOOS == "windows" {
		assert.Contains(t, err.Error(), "Visual C++", "Windows says what a DLL that does not load usually lacks")
	}
}

// The installer puts onnxruntime.dll and onnxruntime_providers_shared.dll next to mam.exe, and mam.exe finds them with
// RuntimeLibraryPath: this lays the real DLL out that way and loads it by that resolved path, with no other setting.
func TestNewONNX_LoadsTheDLLNextToTheExecutableOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the DLL next to mam.exe is the Windows layout")
	}
	models, lib := modelDirFromEnv(t), os.Getenv("MAM_FACESIM_RUNTIME_LIB")
	if lib == "" {
		t.Skip("MAM_FACESIM_RUNTIME_LIB is not set")
	}
	install := t.TempDir()
	for _, name := range []string{facesim.WindowsRuntimeLibraryFile, "onnxruntime_providers_shared.dll"} {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(lib), name)) // #nosec G304 -- test fixture path from the environment
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(install, name), data, 0o600))
	}
	resolved, err := facesim.RuntimeLibraryPath(runtime.GOOS, "", filepath.Join(install, "mam.exe"))
	require.NoError(t, err)
	e := newEngine(t, models, resolved)
	_, err = e.Faces(context.Background(), syntheticImage())
	require.NoError(t, err)
}
