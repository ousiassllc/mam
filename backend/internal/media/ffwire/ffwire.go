// Package ffwire keeps the ffmpeg and ffprobe every service runs (the one media.Tools of the app) the same as what the settings say and the
// lookup finds (Issue #1047), so that setting ffmpeg up, downloading it or removing it applies with no restart.
package ffwire

import (
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/media"
)

// Reload looks for ffmpeg and ffprobe with look (the saved paths, then the search) and hands what it finds to every service that runs
// them (media.Tools.Reload): at the start, after a path was saved, and after ffmpeg was downloaded or removed. Missing is logged, not fatal.
func Reload(tools *media.Tools, look func() (media.Tools, error), logger *slog.Logger) {
	if err := tools.Reload(look); err != nil {
		logger.Warn("ffmpeg / ffprobe not found: Take previews are limited (4K Takes play as they are), and Partial Take Use, the AI Video Judge, the Export and the audio are off until they are set up in the settings", "err", err)
		return
	}
	logger.Info("ffmpeg / ffprobe found", "ffmpeg", tools.FFmpegPath())
}

// Start is the Tools of the app: it holds what look finds now, and again after every call of the function that onChange is given
// (the download of ffmpeg tells it when the copy changes).
func Start(look func() (media.Tools, error), onChange func(func()), logger *slog.Logger) *media.Tools {
	tools := media.NewTools()
	reload := func() { Reload(&tools, look, logger) }
	reload()
	onChange(reload)
	return &tools
}

// Shown is what the settings screen says of ffmpeg, given the paths it found ("" for one that is not found): it makes the services run the
// ffmpeg and ffprobe of that view (and none when it shows none), so that "usable" on the screen and the overview card is what the Judge, the
// previews and the Export can run now. A view that agrees with what the services hold changes nothing.
func Shown(tools *media.Tools, look func() (media.Tools, error), logger *slog.Logger) func(ffmpeg, ffprobe string) {
	return func(ffmpeg, ffprobe string) {
		if ffmpeg != tools.FFmpegPath() || ffprobe != tools.FFprobePath() {
			Reload(tools, look, logger)
		}
	}
}
