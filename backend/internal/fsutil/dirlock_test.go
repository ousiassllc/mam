package fsutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

func TestLockDir_IsExclusiveUntilReleased(t *testing.T) {
	dir := t.TempDir() + "/data"
	first, err := fsutil.LockDir(dir)
	require.NoError(t, err)

	_, err = fsutil.LockDir(dir)
	require.ErrorIs(t, err, fsutil.ErrDirLocked, "a second holder is refused")

	first.Unlock()
	first.Unlock() // releasing twice is harmless
	second, err := fsutil.LockDir(dir)
	require.NoError(t, err, "the lock is free again after Unlock")
	second.Unlock()
	assert.NotPanics(t, func() { (*fsutil.DirLock)(nil).Unlock() })
}
