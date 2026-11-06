package facesim_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// aLibraryThatIsNotONNXRuntime is a shared library every machine of the OS has, which loads but has no ONNX Runtime in it.
func aLibraryThatIsNotONNXRuntime(t *testing.T) string {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		return "kernel32.dll"
	case "darwin":
		return "/usr/lib/libSystem.B.dylib"
	}
	return "libc.so.6"
}

// #1073: a library that loads but is not ONNX Runtime (the bindings panic when it lacks the symbols they look for) is no reason to
// crash the app: NewONNX answers ErrUnavailable, as it does for a library that is missing, and the Judge falls back.
func TestNewONNX_ALibraryThatIsNotONNXRuntimeIsUnavailableNotAPanic(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{facesim.DetectorFile, facesim.RecognizerFile} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("not a model"), 0o600))
	}
	var e *facesim.ONNX
	var err error
	require.NotPanics(t, func() {
		e, err = facesim.NewONNX(facesim.ONNXConfig{ModelDir: dir, RuntimeLibrary: aLibraryThatIsNotONNXRuntime(t)})
	})
	require.Error(t, err)
	assert.Nil(t, e)
	assert.ErrorIs(t, err, facesim.ErrUnavailable)
}
