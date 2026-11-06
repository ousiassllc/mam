package facewire

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// withModels is a Config whose model folder has the two model files (the engine fails at the library, not at the models).
func withModels(t *testing.T) config.Config {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{facesim.DetectorFile, facesim.RecognizerFile} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("not a model"), 0o600))
	}
	return config.Config{FaceModelDir: dir}
}

// noExe is the executable that cannot be found.
func noExe() (string, error) { return "", errors.New("no exe") }

// #1073: where Windows cannot tell where mam.exe is, there is nowhere to look for onnxruntime.dll: the engine is off with the reason
// "the runtime library", and every use answers ErrUnavailable (the Judge falls back to the qualitative evaluation), never a panic or a crash.
func TestEngineIsUnavailableWhenWindowsCannotFindItsDLL(t *testing.T) {
	cfg := withModels(t)
	h := engineOn("windows", cfg, noExe)

	_, err := h.Faces(context.Background(), image.NewRGBA(image.Rect(0, 0, 8, 8)))
	require.ErrorIs(t, err, facesim.ErrUnavailable)
	assert.ErrorContains(t, err, "--onnxruntime-lib", "the way out is told")

	off := h.Off()
	require.NotNil(t, off)
	assert.Equal(t, facesim.ReasonRuntimeMissing, off.Reason)
	assert.Contains(t, off.Detail, "mam.exe")
	assert.False(t, h.Available())
}

func TestEngineIsUnavailableWhenTheModelsAreMissing(t *testing.T) {
	h := engineOn("linux", config.Config{FaceModelDir: t.TempDir()}, noExe)
	off := h.Off()
	require.NotNil(t, off)
	assert.Equal(t, facesim.ReasonModelsMissing, off.Reason)
	assert.ElementsMatch(t, []string{facesim.DetectorFile, facesim.RecognizerFile}, off.Missing)
}

func TestEngineIsUnavailableForALibraryThatIsNotThere(t *testing.T) {
	cfg := withModels(t)
	cfg.ONNXRuntimeLib = filepath.Join(t.TempDir(), "not-there.so")
	h := engineOn("linux", cfg, noExe)
	_, err := h.Faces(context.Background(), image.NewRGBA(image.Rect(0, 0, 8, 8)))
	assert.ErrorIs(t, err, facesim.ErrUnavailable)
	require.NotNil(t, h.Off())
	assert.Equal(t, facesim.ReasonRuntimeMissing, h.Off().Reason)
}
