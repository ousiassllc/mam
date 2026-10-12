package bundlezip

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// archiveWithOneFile builds an Archive whose only asset is <projectID>/a.txt.
func archiveWithOneFile(t *testing.T, projectID string) *Archive {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("a.txt")
	require.NoError(t, err)
	_, err = w.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	return &Archive{
		Data:  domain.BundleData{ProjectID: projectID},
		files: []file{{rel: projectID + "/a.txt", file: zr.File[0], size: 5}},
	}
}

func stubRename(t *testing.T, fn func(oldpath, newpath string) error) {
	t.Helper()
	orig := renameDir
	renameDir = fn
	t.Cleanup(func() { renameDir = orig })
}

func TestInstall_ExistingTargetIsProjectDirExistsWhateverTheRenameError(t *testing.T) {
	cases := map[string]error{
		"linux ENOTEMPTY (fs.ErrExist)":            &os.LinkError{Op: "rename", Err: fs.ErrExist},
		"windows access denied (fs.ErrPermission)": &os.LinkError{Op: "rename", Err: fs.ErrPermission},
	}
	for name, renameErr := range cases {
		t.Run(name, func(t *testing.T) {
			assets := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(assets, "p1"), 0o755))
			stubRename(t, func(string, string) error { return renameErr })

			moved, err := archiveWithOneFile(t, "p1").Install(assets)

			assert.ErrorIs(t, err, ErrProjectDirExists)
			assert.Empty(t, moved)
			entries, err := os.ReadDir(assets)
			require.NoError(t, err)
			require.Len(t, entries, 1, "only the winner's directory is left; staging is removed")
			assert.Equal(t, "p1", entries[0].Name())
		})
	}
}

func TestInstall_RenameFailureWithoutTargetIsNotProjectDirExists(t *testing.T) {
	assets := t.TempDir()
	stubRename(t, func(string, string) error { return &os.LinkError{Op: "rename", Err: fs.ErrPermission} })

	_, err := archiveWithOneFile(t, "p1").Install(assets)

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrProjectDirExists)
}

func TestInstall_MovesFilesIntoPlace(t *testing.T) {
	assets := t.TempDir()

	moved, err := archiveWithOneFile(t, "p1").Install(assets)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(assets, "p1"), moved)
	b, err := os.ReadFile(filepath.Join(moved, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(b))
}
