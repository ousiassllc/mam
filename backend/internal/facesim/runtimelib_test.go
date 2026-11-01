package facesim_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
)

func TestRuntimeLibraryPath(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "mam.exe")

	t.Run("Windows takes the DLL next to mam.exe", func(t *testing.T) {
		lib, err := facesim.RuntimeLibraryPath("windows", "", exe)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(dir, "onnxruntime.dll"), lib)
	})
	t.Run("an explicit library wins everywhere", func(t *testing.T) {
		for _, goos := range []string{"windows", "darwin", "linux"} {
			lib, err := facesim.RuntimeLibraryPath(goos, "/opt/ort/libonnxruntime.so", exe)
			require.NoError(t, err, goos)
			assert.Equal(t, "/opt/ort/libonnxruntime.so", lib, goos)
		}
	})
	t.Run("an explicit library needs no executable", func(t *testing.T) {
		lib, err := facesim.RuntimeLibraryPath("windows", "D:/ort/onnxruntime.dll", "")
		require.NoError(t, err)
		assert.Equal(t, "D:/ort/onnxruntime.dll", lib)
	})
	t.Run("macOS and Linux leave the library to the system search", func(t *testing.T) {
		for _, goos := range []string{"darwin", "linux"} {
			lib, err := facesim.RuntimeLibraryPath(goos, "", exe)
			require.NoError(t, err, goos)
			assert.Empty(t, lib, goos)
		}
	})
	t.Run("Windows without the location of mam.exe fails with the reason", func(t *testing.T) {
		_, err := facesim.RuntimeLibraryPath("windows", "", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--onnxruntime-lib")
	})
}
