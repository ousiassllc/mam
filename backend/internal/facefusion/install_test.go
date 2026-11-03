package facefusion

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media/locate"
)

func layout(t *testing.T) (root, dir, python string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the layout uses POSIX paths")
	}
	root = t.TempDir()
	dir = filepath.Join(root, "facefusion")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "facefusion"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, Script), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "facefusion", "metadata.py"), []byte("METADATA =\\\n{\n\t'version': '3.9.2',\n\t'license': 'OpenRAIL-AS',\n}\n"), 0o600))
	python = filepath.Join(root, "bin", "python3")
	require.NoError(t, os.MkdirAll(filepath.Dir(python), 0o750))
	require.NoError(t, os.WriteFile(python, nil, 0o700)) // #nosec G306 -- a test executable
	return root, dir, python
}

func finder(dirs ...string) Finder {
	return Finder{Finder: locate.Finder{GOOS: "linux", Path: "/nowhere", Usable: locate.IsExecutable}, Dirs: dirs}
}

func TestLookFindsTheConfiguredPythonAndDirectoryAndReadsTheVersion(t *testing.T) {
	_, dir, python := layout(t)

	in, err := finder().Look(Config{Python: python, Path: dir})

	require.NoError(t, err)
	assert.Equal(t, Install{Python: python, Dir: dir, Version: "3.9.2", License: "OpenRAIL-AS"}, in)
}

func TestLookTakesThePathOfFacefusionPyAsItsDirectory(t *testing.T) {
	_, dir, python := layout(t)

	in, err := finder().Look(Config{Python: python, Path: filepath.Join(dir, Script)})

	require.NoError(t, err)
	assert.Equal(t, dir, in.Dir)
}

func TestLookSearchesPathForPythonAndTheListedDirectoriesForFaceFusion(t *testing.T) {
	root, dir, _ := layout(t)
	f := finder(filepath.Join(root, "missing"), dir)
	f.Path = filepath.Join(root, "bin")

	in, err := f.Look(Config{})

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "bin", "python3"), in.Python)
	assert.Equal(t, dir, in.Dir)
}

func TestLookWithNothingFoundSaysWhereItLooked(t *testing.T) {
	root, _, _ := layout(t)

	_, err := finder(filepath.Join(root, "missing")).Look(Config{Python: filepath.Join(root, "bin", "python3")})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), filepath.Join(root, "missing"))

	_, err = finder().Look(Config{Path: filepath.Join(root, "facefusion")})
	require.ErrorIs(t, err, ErrNotFound, "no python on PATH")
	assert.Contains(t, err.Error(), "python3")
}

func TestLookRefusesAConfiguredPathThatCannotBeUsedAndDoesNotFallBack(t *testing.T) {
	root, dir, python := layout(t)
	empty := filepath.Join(root, "empty")
	require.NoError(t, os.Mkdir(empty, 0o750))
	other := filepath.Join(root, "other")
	require.NoError(t, os.MkdirAll(other, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(other, Script), nil, 0o600))

	for name, tc := range map[string]struct {
		cfg   Config
		field string
		err   error
	}{
		"relative python":       {Config{Python: "python", Path: dir}, FieldPython, locate.ErrNotAbsolute},
		"python that is gone":   {Config{Python: filepath.Join(root, "gone"), Path: dir}, FieldPython, locate.ErrNotExecutable},
		"relative directory":    {Config{Python: python, Path: "facefusion"}, FieldPath, locate.ErrNotAbsolute},
		"directory without py":  {Config{Python: python, Path: empty}, FieldPath, ErrNoScript},
		"script of another one": {Config{Python: python, Path: other}, FieldPath, ErrNotFaceFusion},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := finder(dir).Look(tc.cfg) // the search would find a good directory: it must not take over
			var ce *ConfigError
			require.ErrorAs(t, err, &ce)
			assert.Equal(t, tc.field, ce.Field)
			assert.ErrorIs(t, err, tc.err)
		})
	}
}

func TestReadMetadataWithoutAVersionIsEmpty(t *testing.T) {
	_, dir, _ := layout(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "facefusion", "metadata.py"), []byte("METADATA = {}\n"), 0o600))

	version, license := ReadMetadata(dir)

	assert.Empty(t, version)
	assert.Empty(t, license)
}

func TestLookTriesTheCondaEnvironmentPythonBeforePath(t *testing.T) {
	root, dir, onPath := layout(t)
	conda := filepath.Join(root, "miniconda3", "envs", "facefusion", "bin", "python")
	require.NoError(t, os.MkdirAll(filepath.Dir(conda), 0o750))
	require.NoError(t, os.WriteFile(conda, nil, 0o700)) // #nosec G306 -- a test executable
	f := finder(dir)
	f.Path = filepath.Dir(onPath)
	f.Pythons = []string{filepath.Join(root, "anaconda3", "envs", "facefusion", "bin", "python"), conda}

	in, err := f.Look(Config{})

	require.NoError(t, err)
	assert.Equal(t, conda, in.Python, "the first of the candidates that can be run")
}

func TestDefaultFinderListsTheCloneAndCondaPlacesUnderHome(t *testing.T) {
	f := DefaultFinder("/home/me")
	assert.Contains(t, f.Dirs, filepath.Join("/home/me", "facefusion"))
	assert.Contains(t, f.Dirs, filepath.Join("/home/me", "Documents", "facefusion"))
	assert.NotEmpty(t, f.Pythons)
	assert.Empty(t, DefaultFinder("").Dirs)
}
