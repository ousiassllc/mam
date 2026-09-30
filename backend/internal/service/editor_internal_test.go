package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEditorService_AssetPathNeverLeavesTheAssetsDirectory(t *testing.T) {
	base := t.TempDir()
	assets := filepath.Join(base, "assets")
	require.NoError(t, os.MkdirAll(filepath.Join(assets, "p1"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(assets, "p1", "a.mp4"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "secret.mp4"), []byte("x"), 0o600))
	s := &EditorService{core: &core{assetsDir: assets}}

	assert.Equal(t, filepath.Join(assets, "p1", "a.mp4"), s.assetPath("p1/a.mp4"))
	assert.True(t, s.fileExists("p1/a.mp4"))
	for _, rel := range []string{"../secret.mp4", "p1/../../secret.mp4", "../../../etc/passwd", ".."} {
		assert.Empty(t, s.assetPath(rel), rel)
		assert.False(t, s.fileExists(rel), rel)
	}
}
