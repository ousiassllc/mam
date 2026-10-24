package fsutil_test

import (
	"context"
	"testing"
	"time"

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

func TestLockFileBlocking_WaitsForTheHolderAndHonoursTheContext(t *testing.T) {
	path := t.TempDir() + "/sub/.test.lock"
	first, err := fsutil.LockFileBlocking(context.Background(), path)
	require.NoError(t, err)

	expired, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = fsutil.LockFileBlocking(expired, path)
	require.ErrorIs(t, err, context.DeadlineExceeded, "a second holder waits, then gives up with the context")

	got := make(chan *fsutil.FileLock, 1)
	go func() {
		l, err := fsutil.LockFileBlocking(context.Background(), path)
		if err == nil {
			got <- l
		}
	}()
	first.Unlock()
	first.Unlock() // releasing twice is harmless
	select {
	case l := <-got:
		l.Unlock()
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the waiting holder never got the lock after Unlock")
	}
	assert.NotPanics(t, func() { (*fsutil.FileLock)(nil).Unlock() })
}
