// Package audiolen is the length of the audio input of a generation test, for the models whose clip is as long as the sound (Issue #1153; Wan 2.2 S2V with Extend):
// the estimate reads it from the file, so that the confirmation shows the real length and cost, and refuses a sound that is too long or cannot be read as a refused input
// (gterr.InvalidError, the same sentences as direct.Video's). The reading itself is package audiolength of the Model Adapter.
package audiolen

import (
	"os"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/audiolength"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// OfFile is the length in seconds (rounded up to 0.1 s) of the audio file at path; limit is gentest.max_audio_seconds. A sound over the limit
// (「音声が長すぎます（○○ 秒。上限は ○○ 秒）」) or one whose length cannot be read (「音声の長さを読めません」) is a *gterr.InvalidError.
func OfFile(path string, limit int) (float64, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path of a resolved input (ResolveInput): a staged file or a copy in a batch directory
	if err != nil {
		return 0, err
	}
	seconds, err := audiolength.Of(data, limit)
	if ae, ok := apperr.As(err); ok {
		return 0, gterr.Invalid("%s", ae.Cause)
	}
	return seconds, err
}

// Any reports whether one of the models makes its clip as long as its sound (VideoCapabilities.AudioLength): only then is the sound's length read.
func Any(cfg modeladapter.Config, models []modelcatalog.Info) bool {
	return slices.ContainsFunc(models, func(m modelcatalog.Info) bool {
		caps, ok := cfg.VideoCaps(domain.ModelName(m.ID))
		return ok && caps.AudioLength
	})
}

// Seconds is the length in seconds of the audio input (the one of roles that is the role audio; resolve gives the path of the file of reference i) when a chosen
// model makes its clip as long as the sound: the plans of those models carry it as the clip's length. 0 when no such model is chosen or there is no audio.
func Seconds(cfg modeladapter.Config, models []modelcatalog.Info, roles []string, resolve func(i int) (path string, err error)) (float64, error) {
	i := slices.Index(roles, gentestrepo.RoleAudio)
	if i < 0 || !Any(cfg, models) {
		return 0, nil
	}
	path, err := resolve(i)
	if err != nil {
		return 0, err
	}
	return OfFile(path, cfg.Gentest.AudioSeconds())
}
