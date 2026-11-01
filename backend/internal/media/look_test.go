package media

import (
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
		assert.Equal(t, dir+"/ffmpeg", tools.ffmpeg)
		assert.Equal(t, dir+"/ffprobe", tools.ffprobe)
		assert.Equal(t, defaultLimits, tools.lim)
	}
}

func TestLookIn_ConfiguredPathsWinOverTheSearch(t *testing.T) {
	f := finderWith("/opt/homebrew/bin/ffmpeg", "/opt/homebrew/bin/ffprobe", "/mine/ffmpeg")

	tools, err := LookIn(f, locate.Config{FFmpeg: "/mine/ffmpeg"})

	require.NoError(t, err)
	assert.Equal(t, "/mine/ffmpeg", tools.ffmpeg)
	assert.Equal(t, "/opt/homebrew/bin/ffprobe", tools.ffprobe)
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
