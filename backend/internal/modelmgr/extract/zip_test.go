package extract_test

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
)

// makeZip writes a zip with the entries (name -> content; a name ending in "/" is a directory) and returns its path and the sum of the
// content sizes.
func makeZip(t *testing.T, entries map[string]string, order []string) (string, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.zip")
	f, err := os.Create(path)
	require.NoError(t, err)
	w := zip.NewWriter(f)
	var total int64
	for _, name := range order {
		body := entries[name]
		h, err := w.Create(name)
		require.NoError(t, err)
		_, err = h.Write([]byte(body))
		require.NoError(t, err)
		total += int64(len(body))
	}
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
	return path, total
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestZipExtractsBelowTheStrippedRoot(t *testing.T) {
	archive, size := makeZip(t, map[string]string{
		"MuseTalk-abc/":                  "",
		"MuseTalk-abc/scripts/run.py":    "print(1)",
		"MuseTalk-abc/musetalk/utils.py": "x = 2",
	}, []string{"MuseTalk-abc/", "MuseTalk-abc/scripts/run.py", "MuseTalk-abc/musetalk/utils.py"})
	dest := filepath.Join(t.TempDir(), "env")
	var done, total int64
	require.NoError(t, extract.Zip(context.Background(), archive, dest, "MuseTalk-abc", size, func(d, tot int64) { done, total = d, tot }))
	assert.Equal(t, "print(1)", read(t, filepath.Join(dest, "scripts", "run.py")))
	assert.Equal(t, "x = 2", read(t, filepath.Join(dest, "musetalk", "utils.py")))
	assert.Equal(t, size, done)
	assert.Equal(t, size, total)
}

func TestZipWithoutAStripKeepsTheNames(t *testing.T) {
	archive, size := makeZip(t, map[string]string{"python.exe": "MZ", "python310._pth": "x"}, []string{"python.exe", "python310._pth"})
	dest := filepath.Join(t.TempDir(), "env", "python")
	require.NoError(t, extract.Zip(context.Background(), archive, dest, "", size, nil))
	assert.FileExists(t, filepath.Join(dest, "python.exe"))
}

func TestZipRefusesWhatLeavesTheDestinationOrIsNotTheStrippedRoot(t *testing.T) {
	for name, tc := range map[string]struct {
		entries map[string]string
		strip   string
		want    string
	}{
		"a parent directory":       {map[string]string{"../evil": "x"}, "", "outside the destination"},
		"an absolute path":         {map[string]string{"/etc/evil": "x"}, "", "outside the destination"},
		"a drive":                  {map[string]string{"C:/evil": "x"}, "", "outside the destination"},
		"a backslash parent":       {map[string]string{`..\evil`: "x"}, "", "outside the destination"},
		"outside the stripped dir": {map[string]string{"other/file": "x"}, "root", "is not inside"},
	} {
		t.Run(name, func(t *testing.T) {
			for n := range tc.entries {
				archive, size := makeZip(t, tc.entries, []string{n})
				dest := filepath.Join(t.TempDir(), "env")
				err := extract.Zip(context.Background(), archive, dest, tc.strip, size, nil)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.want)
				assert.NoFileExists(t, filepath.Join(filepath.Dir(dest), "evil"))
			}
		})
	}
}

// The catalog pins the size of what a part holds: an archive with more is refused before a byte is written.
func TestZipRefusesAnArchiveThatHoldsMoreThanThePin(t *testing.T) {
	archive, size := makeZip(t, map[string]string{"a": "0123456789"}, []string{"a"})
	dest := filepath.Join(t.TempDir(), "env")
	err := extract.Zip(context.Background(), archive, dest, "", size-1, nil)
	require.ErrorIs(t, err, extract.ErrTooLarge)
	assert.NoFileExists(t, filepath.Join(dest, "a"))
	require.ErrorIs(t, extract.Zip(context.Background(), archive, dest, "", 0, nil), extract.ErrTooLarge, "no pin is no permission")
}

func TestZipNeverOverwritesAFileOfAnotherPart(t *testing.T) {
	archive, size := makeZip(t, map[string]string{"a": "new"}, []string{"a"})
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dest, "a"), []byte("old"), 0o600))
	require.ErrorIs(t, extract.Zip(context.Background(), archive, dest, "", size, nil), extract.ErrExists)
	assert.Equal(t, "old", read(t, filepath.Join(dest, "a")))
}

func TestZipStopsWhenTheContextEnds(t *testing.T) {
	archive, size := makeZip(t, map[string]string{"a": "x"}, []string{"a"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, extract.Zip(ctx, archive, filepath.Join(t.TempDir(), "env"), "", size, nil), context.Canceled)
}

// A wheel is a zip laid out as a package: its files go to site-packages, purelib and platlib of the .data directory go there as well
// and the scripts, headers and data of the .data directory do not.
func TestWheelPutsThePackageAndItsDistInfoInSitePackages(t *testing.T) {
	order := []string{
		"numpy/__init__.py", "numpy/core/_multiarray.pyd", "numpy-1.23.5.dist-info/METADATA", "numpy-1.23.5.dist-info/RECORD",
		"numpy-1.23.5.data/purelib/extra/pure.py", "numpy-1.23.5.data/platlib/extra/native.pyd",
		"numpy-1.23.5.data/scripts/f2py.exe", "numpy-1.23.5.data/data/share/man/f2py.1", "numpy-1.23.5.data/headers/numpy.h",
	}
	entries := map[string]string{}
	for _, n := range order {
		entries[n] = n
	}
	archive, size := makeZip(t, entries, order)
	site := filepath.Join(t.TempDir(), "python", "Lib", "site-packages")
	require.NoError(t, extract.Wheel(context.Background(), archive, site, size, nil))
	for _, want := range []string{"numpy/__init__.py", "numpy/core/_multiarray.pyd", "numpy-1.23.5.dist-info/METADATA", "extra/pure.py", "extra/native.pyd"} {
		assert.FileExists(t, filepath.Join(site, filepath.FromSlash(want)))
	}
	for _, not := range []string{"numpy-1.23.5.data", "f2py.exe", "share", "numpy.h", "scripts", "headers"} {
		assert.NoFileExists(t, filepath.Join(site, not))
	}
}

func TestWheelRefusesAnEntryOutsideSitePackages(t *testing.T) {
	archive, size := makeZip(t, map[string]string{"pkg-1.data/purelib/../../../evil.py": "x"}, []string{"pkg-1.data/purelib/../../../evil.py"})
	site := filepath.Join(t.TempDir(), "site")
	require.Error(t, extract.Wheel(context.Background(), archive, site, size, nil))
	assert.NoFileExists(t, filepath.Join(filepath.Dir(site), "evil.py"))
}
