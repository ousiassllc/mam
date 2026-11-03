package facefusionsettings

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
)

type machine struct{ root, dir, python string }

// install lays out a FaceFusion directory and a python under a temporary directory.
func install(t *testing.T) machine {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the layout uses POSIX paths")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "facefusion")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "facefusion"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, facefusion.Script), nil, 0o600))
	meta := "METADATA =\\\n{\n\t'version': '3.9.2',\n\t'license': 'OpenRAIL-AS',\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "facefusion", "metadata.py"), []byte(meta), 0o600))
	py := filepath.Join(root, "python")
	require.NoError(t, os.WriteFile(py, nil, 0o700)) // #nosec G306 -- a test executable
	return machine{root: root, dir: dir, python: py}
}

func (m machine) service(t *testing.T, dirs ...string) (*Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	f := facefusion.Finder{Finder: locate.Finder{GOOS: "linux", Path: "/nowhere", Usable: locate.IsExecutable}, Dirs: dirs}
	return New(path, "", WithFinder(f)), path
}

func TestSetSavesBothPathsAndLookUsesThemAtOnce(t *testing.T) {
	m := install(t)
	s, _ := m.service(t)

	require.NoError(t, s.Set(facefusion.Config{Python: m.python, Path: m.dir}))
	in, err := s.Look()

	require.NoError(t, err)
	assert.Equal(t, facefusion.Install{Python: m.python, Dir: m.dir, Version: "3.9.2", License: "OpenRAIL-AS"}, in)
	v, err := s.View()
	require.NoError(t, err)
	assert.True(t, v.Ready())
	assert.Equal(t, "3.9.2", v.Version)
	assert.Equal(t, "OpenRAIL-AS", v.License)
	assert.Equal(t, m.python, v.Python.Configured)
}

func TestSetRefusesAPathThatCannotBeUsedAndWritesNothing(t *testing.T) {
	m := install(t)
	s, path := m.service(t)

	err := s.Set(facefusion.Config{Python: m.python, Path: filepath.Join(m.root, "nowhere")})

	var ce *facefusion.ConfigError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, facefusion.FieldPath, ce.Field)
	assert.NoFileExists(t, path)
	require.ErrorAs(t, s.Set(facefusion.Config{Python: "python"}), &ce)
	assert.Equal(t, facefusion.FieldPython, ce.Field)
}

func TestSetEmptyClearsThePathsAndKeepsKeysItDoesNotManage(t *testing.T) {
	m := install(t)
	s, path := m.service(t)
	require.NoError(t, os.WriteFile(path, []byte(`{"other": 1}`), 0o600))
	require.NoError(t, s.Set(facefusion.Config{Python: " " + m.python + " ", Path: m.dir}))

	require.NoError(t, s.Set(facefusion.Config{}))

	cfg, err := s.Config()
	require.NoError(t, err)
	assert.Equal(t, facefusion.Config{}, cfg)
	raw, err := os.ReadFile(path) // #nosec G304 -- a file of the test's temp directory
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"other": 1`)
}

func TestViewWithNothingInstalledSaysWhatIsMissingAndWhereItLooked(t *testing.T) {
	m := install(t)
	missing := filepath.Join(m.root, "gone")
	s, _ := m.service(t, missing)

	v, err := s.View()

	require.NoError(t, err)
	assert.False(t, v.Ready())
	assert.ErrorIs(t, v.Python.Err, facefusion.ErrNotFound)
	assert.ErrorIs(t, v.Path.Err, facefusion.ErrNotFound)
	assert.Equal(t, []string{missing}, v.SearchDirs)
	_, err = s.Look()
	assert.ErrorIs(t, err, facefusion.ErrNotFound)
}

func TestViewFindsTheDirectoryByTheSearchWhenNoneIsSet(t *testing.T) {
	m := install(t)
	s, _ := m.service(t, m.dir)
	require.NoError(t, s.Set(facefusion.Config{Python: m.python}))

	v, err := s.View()

	require.NoError(t, err)
	assert.True(t, v.Ready())
	assert.Empty(t, v.Path.Configured)
	assert.Equal(t, m.dir, v.Path.Found)
}

// A path saved earlier whose file is gone is reported, not replaced by whatever the search finds.
func TestLookWithASavedPathThatWentMissingIsAnErrorNamingIt(t *testing.T) {
	m := install(t)
	s, path := m.service(t, m.dir)
	require.NoError(t, os.WriteFile(path, []byte(`{"python": "`+filepath.Join(m.root, "gone")+`"}`), 0o600))

	_, err := s.Look()

	var ce *facefusion.ConfigError
	require.ErrorAs(t, err, &ce)
	assert.Contains(t, err.Error(), "gone")
}

func TestConfigThatCannotBeReadIsAnError(t *testing.T) {
	m := install(t)
	s, path := m.service(t)
	require.NoError(t, os.WriteFile(path, []byte(`{"python": 3}`), 0o600))

	_, err := s.Config()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"python"`)

	require.NoError(t, os.WriteFile(path, []byte(`not json`), 0o600))
	_, err = s.View()
	require.Error(t, err)
	_, err = s.Look()
	assert.False(t, errors.Is(err, facefusion.ErrNotFound))
}
