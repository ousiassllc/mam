package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

func TestCopyFileCopiesTheBytesAndLeavesTheSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.bin")
	require.NoError(t, os.WriteFile(src, []byte("payload"), 0o600))
	dst := filepath.Join(dir, "sub", "b.bin")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o750))
	require.NoError(t, fsutil.CopyFile(src, dst))
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "payload", string(got))
	assert.FileExists(t, src)
	assert.Error(t, fsutil.CopyFile(filepath.Join(dir, "missing"), dst), "a missing source is an error")
}
