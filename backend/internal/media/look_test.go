package media

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media/locate"
)

// finderWith is a Finder of a Mac started from the Finder (a PATH without Homebrew) that has only the given files.
func finderWith(files ...string) locate.Finder {
	return locate.Finder{GOOS: "darwin", Path: "/usr/bin:/bin:/usr/sbin:/sbin", Usable: func(p string) bool {
		for _, f := range files {
			if f == p {
				return true
			}
		}
		return false
	}}
}

// Issue #856: a MAM.app started from the Finder has no Homebrew directory in its PATH and so could not find the ffmpeg
// that `brew install ffmpeg` put in /opt/homebrew/bin (Apple silicon) or /usr/local/bin (Intel).
func TestLookIn_FindsHomebrewFfmpegThatThePathLacks(t *testing.T) {
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		tools, err := LookIn(finderWith(dir+"/ffmpeg", dir+"/ffprobe"), locate.Config{})

		require.NoError(t, err, dir)
		assert.Equal(t, dir+"/ffmpeg", tools.exes().ffmpeg)
		assert.Equal(t, dir+"/ffprobe", tools.exes().ffprobe)
		assert.Equal(t, defaultLimits, tools.exes().lim)
	}
}

func TestLookIn_ConfiguredPathsWinOverTheSearch(t *testing.T) {
	f := finderWith("/opt/homebrew/bin/ffmpeg", "/opt/homebrew/bin/ffprobe", "/mine/ffmpeg")

	tools, err := LookIn(f, locate.Config{FFmpeg: "/mine/ffmpeg"})

	require.NoError(t, err)
	assert.Equal(t, "/mine/ffmpeg", tools.exes().ffmpeg)
	assert.Equal(t, "/opt/homebrew/bin/ffprobe", tools.exes().ffprobe)
}

// A local runtime ships no ffmpeg: it is told the one that was found (the configured path when there is one).
func TestFFmpegPath_IsTheFfmpegThatWasFound(t *testing.T) {
	tools, err := LookIn(finderWith("/opt/homebrew/bin/ffmpeg", "/opt/homebrew/bin/ffprobe", "/mine/ffmpeg"), locate.Config{FFmpeg: "/mine/ffmpeg"})
	require.NoError(t, err)
	assert.Equal(t, "/mine/ffmpeg", tools.FFmpegPath())
}

func TestLookIn_ConfiguredPathThatCannotBeUsedIsReportedNotReplaced(t *testing.T) {
	f := finderWith("/opt/homebrew/bin/ffmpeg", "/opt/homebrew/bin/ffprobe")

	_, err := LookIn(f, locate.Config{FFprobe: "/gone/ffprobe"})

	require.ErrorIs(t, err, ErrUnavailable)
	var pe *locate.PathError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, "/gone/ffprobe", pe.Path)
	assert.Contains(t, err.Error(), "/gone/ffprobe")
}

func TestLookIn_MissingToolIsUnavailableAndNamesWhereItLooked(t *testing.T) {
	_, err := LookIn(finderWith("/usr/local/bin/ffmpeg"), locate.Config{})

	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, err, locate.ErrNotFound)
	assert.Contains(t, err.Error(), "ffprobe")
	assert.NotContains(t, err.Error(), "ffmpeg not found", "ffmpeg was found")
	assert.Contains(t, err.Error(), "/opt/homebrew/bin")
}

// Issue #1047: a Tools handed to the services at startup follows the ffmpeg that is set up later (Reload), with no restart.
func TestTools_ReloadMakesEveryCopyRunTheFfmpegFoundLater(t *testing.T) {
	tools := NewTools()
	copyOfTools := tools // what a service holds
	assert.False(t, copyOfTools.Available(), "nothing is found at the start")
	_, err := copyOfTools.Probe(t.Context(), "x.mp4")
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = copyOfTools.AudioDuration(t.Context(), "x.wav")
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, copyOfTools.Frames(t.Context(), "x.mp4", "%d.jpg", "fps=1", 1, 1), ErrUnavailable)
	assert.Empty(t, copyOfTools.FFmpegPath())

	require.NoError(t, tools.Reload(func() (Tools, error) {
		return LookIn(finderWith("/opt/homebrew/bin/ffmpeg", "/opt/homebrew/bin/ffprobe"), locate.Config{})
	}))

	assert.True(t, copyOfTools.Available(), "the copy sees what was found after it was made")
	assert.Equal(t, "/opt/homebrew/bin/ffmpeg", copyOfTools.FFmpegPath())
	assert.Equal(t, "/opt/homebrew/bin/ffprobe", copyOfTools.FFprobePath())
}

func TestTools_ReloadThatFindsNothingEmptiesTheToolsAndReportsWhy(t *testing.T) {
	tools := NewTools()
	require.NoError(t, tools.Reload(func() (Tools, error) {
		return LookIn(finderWith("/opt/homebrew/bin/ffmpeg", "/opt/homebrew/bin/ffprobe"), locate.Config{})
	}))
	require.True(t, tools.Available())

	err := tools.Reload(func() (Tools, error) { return LookIn(finderWith(), locate.Config{}) })

	require.ErrorIs(t, err, ErrUnavailable)
	assert.False(t, tools.Available(), "ffmpeg that was removed is not run any more")
	_, err = tools.Probe(t.Context(), "x.mp4")
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestTools_NilAndZeroAreUnavailable(t *testing.T) {
	var nilTools *Tools
	zero := &Tools{}

	assert.False(t, nilTools.Available())
	assert.False(t, zero.Available())
	_, err := zero.Probe(t.Context(), "x.mp4")
	assert.ErrorIs(t, err, ErrUnavailable)
}

// A call that started keeps the ffmpeg it started with: a Reload in the middle does not change it, and Reload racing the calls is safe (run with -race).
func TestTools_ReloadWhileCallsRunIsSafeAndEachCallKeepsItsFfmpeg(t *testing.T) {
	tools := NewTools()
	a, b := finderWith("/a/ffmpeg", "/a/ffprobe"), finderWith("/b/ffmpeg", "/b/ffprobe")
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_ = tools.Reload(func() (Tools, error) {
					if i%2 == 0 {
						return LookIn(a, locate.Config{})
					}
					return LookIn(b, locate.Config{})
				})
				got := tools.exes()
				assert.Equal(t, filepath.Dir(got.ffmpeg), filepath.Dir(got.ffprobe), "a call never sees the ffmpeg of one Reload and the ffprobe of another")
				_ = tools.Available()
			}
		}()
	}
	wg.Wait()
}
