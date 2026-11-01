package ffmpegsettings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
)

// machine is a Finder of a Mac whose app was started from the Finder: the PATH has no Homebrew, and the files are the ones
// given.
func machine(files ...string) locate.Finder {
	return locate.Finder{GOOS: "darwin", Path: "/usr/bin:/bin", Usable: func(p string) bool {
		for _, f := range files {
			if f == p {
				return true
			}
		}
		return false
	}}
}

func newService(t *testing.T, files ...string) (*Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	return New(path, WithFinder(machine(files...))), path
}

func TestLook_FindsHomebrewWhenThePathLacksIt(t *testing.T) {
	s, _ := newService(t, "/opt/homebrew/bin/ffmpeg", "/opt/homebrew/bin/ffprobe")

	_, err := s.Look()

	require.NoError(t, err)
}

func TestLook_NothingInstalledIsUnavailableAndSaysWhereItLooked(t *testing.T) {
	s, _ := newService(t)

	_, err := s.Look()

	require.ErrorIs(t, err, media.ErrUnavailable)
	assert.Contains(t, err.Error(), "/opt/homebrew/bin")
}

func TestSet_SavedPathWinsOverTheSearch(t *testing.T) {
	s, _ := newService(t, "/usr/bin/ffmpeg", "/usr/bin/ffprobe", "/opt/mine/ffmpeg")

	require.NoError(t, s.Set(locate.Config{FFmpeg: "/opt/mine/ffmpeg"}))
	v, err := s.View()

	require.NoError(t, err)
	assert.Equal(t, "/opt/mine/ffmpeg", v.FFmpeg.Path)
	assert.Equal(t, "/opt/mine/ffmpeg", v.FFmpeg.Configured)
	assert.Equal(t, "/usr/bin/ffprobe", v.FFprobe.Path, "the other one is still searched")
	assert.Equal(t, "/opt/mine/ffmpeg", must(s.Find("ffmpeg")), "the first-run guide reads the same")
}

func must(p string, err error) string {
	if err != nil {
		panic(err)
	}
	return p
}

func TestSet_PathThatCannotBeUsedIsRefusedAndNothingIsWritten(t *testing.T) {
	s, path := newService(t, "/usr/bin/ffmpeg", "/usr/bin/ffprobe")

	err := s.Set(locate.Config{FFmpeg: "/opt/gone/ffmpeg"})

	var pe *locate.PathError
	require.ErrorAs(t, err, &pe)
	assert.Contains(t, err.Error(), "/opt/gone/ffmpeg")
	assert.NoFileExists(t, path)
}

func TestSet_RelativePathIsRefused(t *testing.T) {
	s, _ := newService(t, "/usr/bin/ffmpeg")

	var pe *locate.PathError
	require.ErrorAs(t, s.Set(locate.Config{FFmpeg: "ffmpeg"}), &pe)
}

func TestSet_EmptyClearsThePathAndRemovesTheFile(t *testing.T) {
	s, path := newService(t, "/opt/mine/ffmpeg")
	require.NoError(t, s.Set(locate.Config{FFmpeg: "/opt/mine/ffmpeg"}))
	require.FileExists(t, path)

	require.NoError(t, s.Set(locate.Config{}))

	cfg, err := s.Config()
	require.NoError(t, err)
	assert.Equal(t, locate.Config{}, cfg)
	assert.NoFileExists(t, path)
}

func TestSet_KeepsKeysItDoesNotManage(t *testing.T) {
	s, path := newService(t, "/opt/mine/ffmpeg")
	require.NoError(t, os.WriteFile(path, []byte(`{"other": 1}`), 0o600))

	require.NoError(t, s.Set(locate.Config{FFmpeg: " /opt/mine/ffmpeg "}))

	b, err := os.ReadFile(path) // #nosec G304 -- a file of the test's temp directory
	require.NoError(t, err)
	assert.Contains(t, string(b), `"other": 1`)
	assert.Contains(t, string(b), `"ffmpeg": "/opt/mine/ffmpeg"`)
}

// A path saved earlier whose file is gone is reported, not replaced by whatever the search finds.
func TestLook_SavedPathThatWentMissingIsAnErrorNamingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	require.NoError(t, os.WriteFile(path, []byte(`{"ffmpeg": "/opt/mine/ffmpeg"}`), 0o600))
	s := New(path, WithFinder(machine("/usr/bin/ffmpeg", "/usr/bin/ffprobe")))

	_, err := s.Look()

	require.ErrorIs(t, err, media.ErrUnavailable)
	var pe *locate.PathError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, "/opt/mine/ffmpeg", pe.Path)
	v, viewErr := s.View()
	require.NoError(t, viewErr)
	assert.ErrorAs(t, v.FFmpeg.Err, &pe)
	assert.Empty(t, v.FFmpeg.Path)
}

func TestConfig_DamagedFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	require.NoError(t, os.WriteFile(path, []byte(`{`), 0o600))
	s := New(path, WithFinder(machine()))

	_, err := s.Config()
	require.Error(t, err)
	_, err = s.View()
	require.Error(t, err)
	_, err = s.Look()
	require.ErrorIs(t, err, media.ErrUnavailable)
	// The first-run guide only reports what is found: a damaged file leaves it to the search.
	_, err = s.Find("ffmpeg")
	assert.True(t, errors.Is(err, locate.ErrNotFound))
}

func TestConfig_NonStringPathIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	require.NoError(t, os.WriteFile(path, []byte(`{"ffmpeg": 3}`), 0o600))

	_, err := New(path).Config()

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"ffmpeg"`)
}

func TestView_ListsTheSearchDirectories(t *testing.T) {
	s, _ := newService(t)

	v, err := s.View()

	require.NoError(t, err)
	assert.Equal(t, []string{"/usr/bin", "/bin", "/opt/homebrew/bin", "/usr/local/bin"}, v.SearchDirs)
	assert.ErrorIs(t, v.FFmpeg.Err, locate.ErrNotFound)
}

// The downloaded ffmpeg (Issue #900) is searched after everything else: one the user installed always wins.
func TestManagedDirs_TheDownloadedCopyIsFoundWhenNothingElseIs(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s := New(path, WithFinder(machine("/data/ffmpeg/bin/ffmpeg", "/data/ffmpeg/bin/ffprobe")), WithManagedDirs(func() []string { return []string{"/data/ffmpeg/bin"} }))

	_, err := s.Look()
	require.NoError(t, err)
	found, err := s.Find("ffmpeg")
	require.NoError(t, err)
	assert.Equal(t, "/data/ffmpeg/bin/ffmpeg", found)
	v, err := s.View()
	require.NoError(t, err)
	assert.Equal(t, "/data/ffmpeg/bin/ffprobe", v.FFprobe.Path)
	assert.Equal(t, []string{"/usr/bin", "/bin", "/opt/homebrew/bin", "/usr/local/bin", "/data/ffmpeg/bin"}, v.SearchDirs, "last, after PATH and Homebrew")
}

func TestManagedDirs_AnFFmpegTheUserInstalledWinsOverTheDownloadedOne(t *testing.T) {
	files := []string{"/data/ffmpeg/bin/ffmpeg", "/data/ffmpeg/bin/ffprobe", "/opt/homebrew/bin/ffmpeg", "/opt/homebrew/bin/ffprobe", "/mine/ffmpeg"}
	path := filepath.Join(t.TempDir(), FileName)
	s := New(path, WithFinder(machine(files...)), WithManagedDirs(func() []string { return []string{"/data/ffmpeg/bin"} }))

	v, err := s.View()
	require.NoError(t, err)
	assert.Equal(t, "/opt/homebrew/bin/ffmpeg", v.FFmpeg.Path, "Homebrew's, not the managed copy")

	require.NoError(t, s.Set(locate.Config{FFmpeg: "/mine/ffmpeg"}))
	v, err = s.View()
	require.NoError(t, err)
	assert.Equal(t, "/mine/ffmpeg", v.FFmpeg.Path, "the configured path wins over both")
	assert.Equal(t, "/opt/homebrew/bin/ffprobe", v.FFprobe.Path)
}

func TestManagedDirs_AreReadOnEveryLookup(t *testing.T) {
	var dirs []string
	path := filepath.Join(t.TempDir(), FileName)
	s := New(path, WithFinder(machine("/data/ffmpeg/bin/ffmpeg", "/data/ffmpeg/bin/ffprobe")), WithManagedDirs(func() []string { return dirs }))

	_, err := s.Look()
	require.ErrorIs(t, err, media.ErrUnavailable, "before the download")
	_, err = s.Find("ffmpeg")
	require.Error(t, err)

	dirs = []string{"/data/ffmpeg/bin"} // the download finished
	_, err = s.Look()
	require.NoError(t, err, "found by the next lookup, with no new service")
	v, err := s.View()
	require.NoError(t, err)
	assert.Equal(t, "/data/ffmpeg/bin/ffmpeg", v.FFmpeg.Path)
}

func TestManagedDirs_DoNotChangeTheFinderOfTheService(t *testing.T) {
	f := machine()
	f.Extra = []string{"/mine/extra"}
	s := New(filepath.Join(t.TempDir(), FileName), WithFinder(f), WithManagedDirs(func() []string { return []string{"/data/ffmpeg/bin"} }))
	v, err := s.View()
	require.NoError(t, err)
	_, err = s.View()
	require.NoError(t, err)
	assert.Equal(t, []string{"/usr/bin", "/bin", "/opt/homebrew/bin", "/usr/local/bin", "/mine/extra", "/data/ffmpeg/bin"}, v.SearchDirs)
	assert.Equal(t, []string{"/mine/extra"}, s.finder.Extra, "the extra directories are copied, not appended in place")
}
