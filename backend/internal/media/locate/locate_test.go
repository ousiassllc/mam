package locate

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// launchdPath is the PATH of an app started from the Finder or the Dock (Issue #856): it has no Homebrew directory.
const launchdPath = "/usr/bin:/bin:/usr/sbin:/sbin"

func TestSearchDirs_DarwinAddsHomebrewAfterPath(t *testing.T) {
	got := SearchDirs("darwin", launchdPath, nil)

	assert.Equal(t, []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin", "/opt/homebrew/bin", "/usr/local/bin"}, got)
}

func TestSearchDirs_PathComesFirstAndHasNoRepeats(t *testing.T) {
	got := SearchDirs("darwin", "/usr/local/bin:/opt/x:/usr/local/bin", nil)

	assert.Equal(t, []string{"/usr/local/bin", "/opt/x", "/opt/homebrew/bin"}, got)
}

func TestSearchDirs_OtherSystemsSearchPathOnly(t *testing.T) {
	assert.Equal(t, []string{"/usr/bin", "/bin"}, SearchDirs("linux", "/usr/bin:/bin", nil))
}

func TestSearchDirs_ExtraComesLast(t *testing.T) {
	got := SearchDirs("darwin", "/usr/bin", []string{"/data/ffmpeg"})

	assert.Equal(t, []string{"/usr/bin", "/opt/homebrew/bin", "/usr/local/bin", "/data/ffmpeg"}, got)
}

func TestSearchDirs_LeavesOutEmptyAndRelativeEntries(t *testing.T) {
	got := SearchDirs("linux", ":bin:./tools:/usr/bin:", []string{"relative"})

	assert.Equal(t, []string{"/usr/bin"}, got)
}

func TestSearchDirs_Windows(t *testing.T) {
	got := SearchDirs("windows", `C:\Windows;"D:\Program Files\ff";\\server\share;relative`, []string{`C:\mam\ffmpeg`})

	assert.Equal(t, []string{`C:\Windows`, `D:\Program Files\ff`, `\\server\share`, `C:\mam\ffmpeg`}, got)
}

func TestCandidates(t *testing.T) {
	assert.Equal(t, []string{"/a/ffmpeg", "/b/ffmpeg"}, Candidates(FFmpeg, "darwin", []string{"/a", "/b"}))
	assert.Equal(t, []string{`C:\a\ffprobe.exe`}, Candidates(FFprobe, "windows", []string{`C:\a`}))
}

// filesystem is a Finder.Usable over a fixed set of files.
func filesystem(files ...string) func(string) bool {
	return func(p string) bool {
		for _, f := range files {
			if f == p {
				return true
			}
		}
		return false
	}
}

func TestFind_MacAppFindsHomebrewWhenPathLacksIt(t *testing.T) {
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		f := Finder{GOOS: "darwin", Path: launchdPath, Usable: filesystem(dir+"/ffmpeg", dir+"/ffprobe")}

		ffmpeg, err := f.Find(FFmpeg, "")
		require.NoError(t, err, dir)
		ffprobe, err := f.Find(FFprobe, "")
		require.NoError(t, err, dir)

		assert.Equal(t, dir+"/ffmpeg", ffmpeg)
		assert.Equal(t, dir+"/ffprobe", ffprobe)
	}
}

func TestFind_PathWinsOverHomebrew(t *testing.T) {
	f := Finder{GOOS: "darwin", Path: "/custom/bin", Usable: filesystem("/opt/homebrew/bin/ffmpeg", "/custom/bin/ffmpeg")}

	got, err := f.Find(FFmpeg, "")

	require.NoError(t, err)
	assert.Equal(t, "/custom/bin/ffmpeg", got)
}

func TestFind_ConfiguredPathWinsOverSearch(t *testing.T) {
	f := Finder{GOOS: "darwin", Path: "/usr/bin", Usable: filesystem("/usr/bin/ffmpeg", "/opt/mine/ff")}

	got, err := f.Find(FFmpeg, "/opt/mine/ff")

	require.NoError(t, err)
	assert.Equal(t, "/opt/mine/ff", got)
}

func TestFind_ConfiguredPathThatIsNotUsableIsAnErrorNotAFallback(t *testing.T) {
	f := Finder{GOOS: "darwin", Path: "/usr/bin", Usable: filesystem("/usr/bin/ffmpeg")}

	_, err := f.Find(FFmpeg, "/opt/gone/ffmpeg")

	var pe *PathError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, FFmpeg, pe.Name)
	assert.Equal(t, "/opt/gone/ffmpeg", pe.Path)
	assert.ErrorIs(t, err, ErrNotExecutable)
	assert.Contains(t, err.Error(), "/opt/gone/ffmpeg")
	assert.False(t, errors.Is(err, ErrNotFound))
}

func TestFind_ConfiguredRelativePathIsRefused(t *testing.T) {
	f := Finder{GOOS: "linux", Usable: func(string) bool { return true }}

	_, err := f.Find(FFprobe, "bin/ffprobe")

	var pe *PathError
	require.ErrorAs(t, err, &pe)
	assert.ErrorIs(t, pe, ErrNotAbsolute)
}

func TestFind_NotFoundListsWhereItLooked(t *testing.T) {
	f := Finder{GOOS: "darwin", Path: launchdPath, Usable: filesystem()}

	_, err := f.Find(FFmpeg, "")

	require.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), "/opt/homebrew/bin")
	assert.Contains(t, err.Error(), "/usr/local/bin")
}

func TestFind_ExtraDirectoryIsSearchedLast(t *testing.T) {
	f := Finder{GOOS: "linux", Path: "/usr/bin", Extra: []string{"/data/ffmpeg"}, Usable: filesystem("/data/ffmpeg/ffmpeg")}

	got, err := f.Find(FFmpeg, "")

	require.NoError(t, err)
	assert.Equal(t, "/data/ffmpeg/ffmpeg", got)
}

func TestFind_ZeroFinderFindsNothing(t *testing.T) {
	_, err := Finder{GOOS: "linux", Path: "/usr/bin"}.Find(FFmpeg, "")

	require.ErrorIs(t, err, ErrNotFound)
}

func writeFile(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("x"), mode)) // #nosec G306 -- the test needs an executable file
	require.NoError(t, os.Chmod(p, mode))                  // #nosec G302 -- same
	return p
}

func TestIsExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no execute bit")
	}
	dir := t.TempDir()
	exe := writeFile(t, dir, "run", 0o755)
	data := writeFile(t, dir, "data", 0o644)

	assert.True(t, IsExecutable(exe))
	assert.False(t, IsExecutable(data), "no execute bit")
	assert.False(t, IsExecutable(dir), "a directory")
	assert.False(t, IsExecutable(dir+"/missing"))
}
