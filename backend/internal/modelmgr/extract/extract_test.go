package extract_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
)

const root = "ComfyUI_windows_portable"

// hashes is the SHA-256 of every file below dir, by slash path.
func hashes(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		require.NoError(t, err)
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	}))
	return out
}

// expected reads testdata/expected.sha256 ("<sha>  ./<path>" per file of the tree the archive was made from).
func expected(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open("testdata/expected.sha256")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		require.Len(t, fields, 2)
		out[strings.TrimPrefix(fields[1], "./")] = fields[0]
	}
	return out
}

// testdata/bcj2.7z was made with the 7-Zip command line and the method flags of the upstream ComfyUI
// portable archives (windows_release_package.yml): `7z a -t7z -m0=lzma2 -mx=9 -mfb=128 -md=768m -ms=on
// -mf=BCJ2` over a tree with the same top directory, an executable (BCJ2 applies to it) and an empty file.
func TestExtractsAnArchiveMadeWithTheUpstreamMethodFlags(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "engine")
	var done, total int64
	err := extract.SevenZip{}.Extract(context.Background(), "testdata/bcj2.7z", dest, root, func(d, all int64) { done, total = d, all })
	require.NoError(t, err)
	assert.Equal(t, expected(t), hashes(t, dest), "every file has the content it was archived with, below the stripped root")
	assert.Positive(t, total)
	assert.Equal(t, total, done)
	assert.DirExists(t, filepath.Join(dest, "ComfyUI", "models"))
}

// The same flags on this host's 7z (when it has one), with a tree made on the fly: the archive uses BCJ2.
func TestExtractsAnArchiveBuiltWithTheLocal7z(t *testing.T) {
	bin, err := exec.LookPath("7z")
	if err != nil {
		t.Skip("no 7z on this host")
	}
	src := t.TempDir()
	tree := filepath.Join(src, root, "python_embeded") //nolint:misspell // the directory name of the upstream package
	require.NoError(t, os.MkdirAll(tree, 0o750))
	exe, err := os.ReadFile(bin)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(tree, "python.exe"), exe, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tree, "note.txt"), []byte("hello"), 0o600))
	archive := filepath.Join(t.TempDir(), "a.7z")
	cmd := exec.Command(bin, "a", "-t7z", "-m0=lzma2", "-mx=9", "-mfb=128", "-md=768m", "-ms=on", "-mf=BCJ2", archive, root) // #nosec G204 -- test
	cmd.Dir = src
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	dest := filepath.Join(t.TempDir(), "engine")
	require.NoError(t, extract.SevenZip{}.Extract(context.Background(), archive, dest, root, nil))
	assert.Equal(t, hashes(t, filepath.Join(src, root)), hashes(t, dest))
}

func TestExtractStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := extract.SevenZip{}.Extract(ctx, "testdata/bcj2.7z", filepath.Join(t.TempDir(), "e"), root, nil)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestExtractRefusesAnArchiveWithAnotherRoot(t *testing.T) {
	err := extract.SevenZip{}.Extract(context.Background(), "testdata/bcj2.7z", filepath.Join(t.TempDir(), "e"), "Other", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not inside")
}

func TestExtractRefusesATruncatedOrMissingArchive(t *testing.T) {
	data, err := os.ReadFile("testdata/bcj2.7z")
	require.NoError(t, err)
	cut := filepath.Join(t.TempDir(), "cut.7z")
	require.NoError(t, os.WriteFile(cut, data[:len(data)/2], 0o600))
	assert.Error(t, extract.SevenZip{}.Extract(context.Background(), cut, filepath.Join(t.TempDir(), "e"), root, nil))
	assert.Error(t, extract.SevenZip{}.Extract(context.Background(), filepath.Join(t.TempDir(), "none.7z"), filepath.Join(t.TempDir(), "e"), root, nil))
}

func TestEntryPathRefusesWhatLeavesTheDestination(t *testing.T) {
	for _, name := range []string{"../evil", root + "/../../evil", "/abs/x", `C:\x`, root + `/..\..\evil`} {
		_, _, err := extract.EntryPath("/dest", root, name)
		assert.ErrorIs(t, err, extract.ErrUnsafePath, name)
	}
	p, skip, err := extract.EntryPath("/dest", root, root+"/a/b.txt")
	require.NoError(t, err)
	assert.False(t, skip)
	assert.Equal(t, filepath.FromSlash("/dest/a/b.txt"), p)
	_, skip, _ = extract.EntryPath("/dest", root, root)
	assert.True(t, skip)
}
