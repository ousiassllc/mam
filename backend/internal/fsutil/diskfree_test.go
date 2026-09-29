package fsutil_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

func TestFreeBytes_ReadsTheVolumeOfAnExistingOrNotYetCreatedPath(t *testing.T) {
	dir := t.TempDir()
	free, err := fsutil.FreeBytes(dir)
	require.NoError(t, err)
	assert.Positive(t, free)

	// The assets directory may not exist yet on first start: the nearest existing parent answers.
	_, err = fsutil.FreeBytes(filepath.Join(dir, "assets", "not", "created"))
	assert.NoError(t, err)
}
