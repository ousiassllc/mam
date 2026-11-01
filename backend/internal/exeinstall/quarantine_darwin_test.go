package exeinstall

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// TestClearQuarantineRemovesTheAttribute runs on macOS only (an extended attribute needs the file system of a Mac).
func TestClearQuarantineRemovesTheAttribute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o700))

	require.NoError(t, ClearQuarantine(path), "a file without the attribute is the normal case")

	require.NoError(t, unix.Setxattr(path, quarantineAttr, []byte("0083;00000000;Safari;"), 0))
	require.NoError(t, ClearQuarantine(path))
	_, err := unix.Getxattr(path, quarantineAttr, nil)
	assert.ErrorIs(t, err, unix.ENOATTR, "the attribute must be gone")

	assert.Error(t, ClearQuarantine(filepath.Join(t.TempDir(), "missing")), "a file that is not there is an error, not a silent success")
}
