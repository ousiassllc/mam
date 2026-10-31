//go:build !windows

package desktop

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Running from a translocated copy: refused before the file system is touched.
func TestMacUpdatableBundle_TranslocatedCopyIsRefusedWithoutProbing(t *testing.T) {
	probeNotCalled(t)
	_, err := MacUpdatableBundle("/private/var/folders/ab/c/T/AppTranslocation/0E3A/d/MAM.app/Contents/MacOS/mam")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/Applications", "the message tells where to move the app")
}

func TestMacUpdatableBundle(t *testing.T) {
	// A bundle in a folder the user can write to.
	dir := t.TempDir()
	exe := filepath.Join(dir, "MAM.app", "Contents", "MacOS", "mam")
	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0o750))
	got, err := MacUpdatableBundle(exe)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "MAM.app"), got)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the writability probe leaves nothing behind")

	// Not inside a bundle at all.
	_, err = MacUpdatableBundle(filepath.Join(dir, "mam"))
	require.Error(t, err)

	// A folder that is not writable (root ignores the mode).
	if os.Geteuid() != 0 {
		require.NoError(t, os.Chmod(dir, 0o500))       // #nosec G302 -- the test makes the folder read-only
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) // #nosec G302 -- so TempDir can remove it
		_, err = MacUpdatableBundle(exe)
		require.Error(t, err)
	}
}

// probeNotCalled makes the writability probe fail the test when it runs, until the test ends.
func probeNotCalled(t *testing.T) {
	t.Helper()
	orig := createUpdateProbe
	createUpdateProbe = func(dir string) (*os.File, error) {
		t.Errorf("the writability probe ran in %s", dir)
		return nil, os.ErrInvalid
	}
	t.Cleanup(func() { createUpdateProbe = orig })
}

// Where the bundle sits under /Volumes decides nothing: the probe does. The mounted dmg is a read-only volume (EROFS),
// a writable external drive is a folder like any other. macOS itself is not available here: the probe is replaced by
// the answer each kind of volume gives (the real probe: TestMacUpdatableBundle, and macOS in ci.yml's `macos` job).
func TestMacUpdatableBundle_UnderVolumesTheProbeDecides(t *testing.T) {
	orig := createUpdateProbe
	t.Cleanup(func() { createUpdateProbe = orig })
	probed := ""
	answer := func(err error) {
		probed = ""
		createUpdateProbe = func(dir string) (*os.File, error) {
			probed = dir
			if err != nil {
				return nil, &os.PathError{Op: "open", Path: filepath.Join(dir, ".mam-update-probe-1"), Err: err}
			}
			return os.CreateTemp(t.TempDir(), "probe-*") // the folder of a real volume cannot be used: any writable one
		}
	}

	// The dmg the release ships: read-only, so the update is off and the message tells where to move the app.
	answer(syscall.EROFS)
	_, err := MacUpdatableBundle("/Volumes/MAM/MAM.app/Contents/MacOS/mam")
	require.ErrorIs(t, err, syscall.EROFS)
	assert.Equal(t, "/Volumes/MAM", probed, "the folder next to the bundle is probed")
	assert.Contains(t, err.Error(), "/Applications")

	// An external drive the user can write to: the update stays on.
	answer(nil)
	got, err := MacUpdatableBundle("/Volumes/External/Apps/MAM.app/Contents/MacOS/mam")
	require.NoError(t, err)
	assert.Equal(t, "/Volumes/External/Apps/MAM.app", got)
	assert.Equal(t, "/Volumes/External/Apps", probed)

	// A volume this user cannot write to (not the dmg: mounted read-write by someone else, or a read-only share).
	answer(syscall.EACCES)
	_, err = MacUpdatableBundle("/Volumes/Share/MAM.app/Contents/MacOS/mam")
	require.ErrorIs(t, err, syscall.EACCES)
}
