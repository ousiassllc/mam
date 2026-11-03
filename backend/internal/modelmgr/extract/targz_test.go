package extract_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
)

// tarEntry is one entry of a test archive: a directory when Typeflag is tar.TypeDir, a link to Link for a link type, else a file.
type tarEntry struct {
	Name     string
	Body     string
	Mode     int64
	Typeflag byte
	Link     string
}

func makeTarGz(t *testing.T, entries ...tarEntry) (path string, size int64) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "a.tar.gz")
	f, err := os.Create(path)
	require.NoError(t, err)
	zw := gzip.NewWriter(f)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		flag := e.Typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}
		h := &tar.Header{Name: e.Name, Mode: e.Mode, Typeflag: flag, Linkname: e.Link}
		if flag == tar.TypeReg {
			h.Size = int64(len(e.Body))
			size += h.Size
		}
		require.NoError(t, tw.WriteHeader(h))
		if flag == tar.TypeReg {
			_, err = tw.Write([]byte(e.Body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	return path, size
}

// The Python of python-build-standalone: a top directory, files with the execute bit, and aliases that are links.
func TestTarGzExtractsAPythonTreeKeepingTheExecuteBitAndSkippingLinks(t *testing.T) {
	archive, size := makeTarGz(t,
		tarEntry{Name: "python/", Typeflag: tar.TypeDir, Mode: 0o755},
		tarEntry{Name: "python/bin/", Typeflag: tar.TypeDir, Mode: 0o755},
		tarEntry{Name: "python/bin/python3.13", Body: "\xcf\xfa\xed\xfe", Mode: 0o775},
		tarEntry{Name: "python/bin/python", Typeflag: tar.TypeSymlink, Link: "python3.13"},
		tarEntry{Name: "python/lib/python3.13/os.py", Body: "import sys", Mode: 0o644},
	)
	dest := filepath.Join(t.TempDir(), "engine")
	var done, total int64
	require.NoError(t, extract.TarGz(context.Background(), archive, dest, "", size, func(d, tot int64) { done, total = d, tot }))
	assert.Equal(t, "import sys", read(t, filepath.Join(dest, "python", "lib", "python3.13", "os.py")))
	assert.NoFileExists(t, filepath.Join(dest, "python", "bin", "python"), "an alias link is not created")
	assert.Equal(t, size, done)
	assert.Equal(t, size, total)
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(dest, "python", "bin", "python3.13"))
		require.NoError(t, err)
		assert.NotZero(t, st.Mode()&0o100, "the interpreter must be runnable")
		st, err = os.Stat(filepath.Join(dest, "python", "lib", "python3.13", "os.py"))
		require.NoError(t, err)
		assert.Zero(t, st.Mode()&0o111, "a plain file is not made executable")
	}
}

func TestTarGzStripsTheTopDirectory(t *testing.T) {
	archive, size := makeTarGz(t,
		tarEntry{Name: "python/", Typeflag: tar.TypeDir, Mode: 0o755},
		tarEntry{Name: "python/bin/python3.13", Body: "x", Mode: 0o755},
	)
	dest := filepath.Join(t.TempDir(), "py")
	require.NoError(t, extract.TarGz(context.Background(), archive, dest, "python", size, nil))
	assert.FileExists(t, filepath.Join(dest, "bin", "python3.13"))
	require.Error(t, extract.TarGz(context.Background(), archive, filepath.Join(t.TempDir(), "x"), "other", size, nil))
}

func TestTarGzRefusesWhatLeavesTheDestinationOrIsNotAPlainFile(t *testing.T) {
	for name, tc := range map[string]struct {
		entry tarEntry
		want  string
	}{
		"a parent directory": {tarEntry{Name: "../evil", Body: "x", Mode: 0o644}, "outside the destination"},
		"an absolute path":   {tarEntry{Name: "/etc/evil", Body: "x", Mode: 0o644}, "outside the destination"},
		"a drive":            {tarEntry{Name: "C:/evil", Body: "x", Mode: 0o644}, "outside the destination"},
		"a hard link":        {tarEntry{Name: "python/hard", Typeflag: tar.TypeLink, Link: "python/x"}, "not a regular file"},
		"a device":           {tarEntry{Name: "python/dev", Typeflag: tar.TypeChar}, "not a regular file"},
	} {
		t.Run(name, func(t *testing.T) {
			archive, size := makeTarGz(t, tc.entry)
			dest := filepath.Join(t.TempDir(), "env")
			err := extract.TarGz(context.Background(), archive, dest, "", size+1, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NoFileExists(t, filepath.Join(filepath.Dir(dest), "evil"))
		})
	}
}

// The catalog pins the size of what the archive holds: the sum of the headers is checked as they are read, and an entry that would
// cross it is not written.
func TestTarGzRefusesAnArchiveThatHoldsMoreThanThePin(t *testing.T) {
	archive, size := makeTarGz(t,
		tarEntry{Name: "a", Body: "0123456789", Mode: 0o644},
		tarEntry{Name: "b", Body: "0123456789", Mode: 0o644},
	)
	dest := filepath.Join(t.TempDir(), "env")
	err := extract.TarGz(context.Background(), archive, dest, "", size-1, nil)
	require.ErrorIs(t, err, extract.ErrTooLarge)
	assert.NoFileExists(t, filepath.Join(dest, "b"))
	require.ErrorIs(t, extract.TarGz(context.Background(), archive, filepath.Join(t.TempDir(), "x"), "", 0, nil), extract.ErrTooLarge, "no pin is no permission")
}

func TestTarGzNeverOverwritesAFileOfAnotherPart(t *testing.T) {
	archive, size := makeTarGz(t, tarEntry{Name: "a", Body: "new", Mode: 0o644})
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dest, "a"), []byte("old"), 0o600))
	require.ErrorIs(t, extract.TarGz(context.Background(), archive, dest, "", size, nil), extract.ErrExists)
	assert.Equal(t, "old", read(t, filepath.Join(dest, "a")))
}

func TestTarGzStopsWhenTheContextEnds(t *testing.T) {
	archive, size := makeTarGz(t, tarEntry{Name: "a", Body: "x", Mode: 0o644})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, extract.TarGz(ctx, archive, filepath.Join(t.TempDir(), "env"), "", size, nil), context.Canceled)
}

func TestTarGzRefusesAFileThatIsNotGzip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	require.NoError(t, os.WriteFile(path, []byte("not gzip"), 0o600))
	require.Error(t, extract.TarGz(context.Background(), path, filepath.Join(t.TempDir(), "env"), "", 10, nil))
}

// A wheel of macOS (or Linux) keeps the execute bit of its extension modules and helper programs; a zip that carries none (Windows) leaves
// every file a plain one.
func TestZipKeepsTheExecuteBitOfAUnixArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no execute bit")
	}
	path := filepath.Join(t.TempDir(), "a.zip")
	f, err := os.Create(path)
	require.NoError(t, err)
	w := zip.NewWriter(f)
	for name, mode := range map[string]os.FileMode{"bin/tool": 0o755, "pkg/mod.py": 0o644} {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(mode)
		out, err := w.CreateHeader(h)
		require.NoError(t, err)
		_, err = out.Write([]byte("x"))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
	dest := filepath.Join(t.TempDir(), "env")
	require.NoError(t, extract.Zip(context.Background(), path, dest, "", 2, nil))
	tool, err := os.Stat(filepath.Join(dest, "bin", "tool"))
	require.NoError(t, err)
	assert.NotZero(t, tool.Mode()&0o100)
	mod, err := os.Stat(filepath.Join(dest, "pkg", "mod.py"))
	require.NoError(t, err)
	assert.Zero(t, mod.Mode()&0o111)
}
