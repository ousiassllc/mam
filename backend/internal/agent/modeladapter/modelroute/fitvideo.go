package modelroute

import (
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// notGiven is the reason of a model that cannot run without an input mam does not hand it.
const notGiven = "mam が渡さない入力（最後のフレーム・参照画像・音声）が必須"

// fitsVideo is fits for a video model: the first condition of the Shot the model does not meet, in a fixed order ("" when it meets them all).
//
// A Shot with audio (n.audio, Issue #1153) goes only to a model that takes the sound, and such a model is given what it needs: a speech-to-video workflow without a first frame
// reads the Keyframe as its one reference image (modeladapter.Prepare), so the Keyframe is no reason to refuse it, and its required reference image is met by any image of the
// Reference Pack. Its clip is as long as the sound (caps.AudioLength), so the Shot's planned length is no limit for it (the limit is gentest.max_audio_seconds, checked where the
// sound is read). A Shot without audio never goes to a model that requires one.
func (e Env) fitsVideo(m domain.ModelName, n need) string {
	cfg := e.Config
	caps, ok := cfg.VideoCaps(m)
	switch {
	case !ok:
		return "動画モデルではない"
	case n.audio && !caps.AudioInput:
		return "音声を使えない"
	case !caps.SupportsResolution(n.res):
		return fmt.Sprintf("解像度 %s に対応しない", n.res)
	case n.seconds > 0 && !caps.AudioLength && len(caps.Durations) > 0 && caps.Durations[len(caps.Durations)-1] < n.seconds-1e-9:
		return fmt.Sprintf("尺 %.0f 秒を 1 本に収められない（最長 %.0f 秒）", n.seconds, caps.Durations[len(caps.Durations)-1])
	case n.keyframe && !caps.FirstFrame && (!n.audio || caps.MaxReferenceImages <= 0):
		return "最初のフレーム（Keyframe）を使えない"
	case !n.keyframe && requires(cfg, m, localcfg.RequiredFirstFrame):
		return "最初のフレームが必要（この Shot には Keyframe がない）"
	case n.audio && !n.reference && requires(cfg, m, localcfg.RequiredReference):
		return "参照画像が必要（この Shot には Keyframe も参照画像もない）"
	case requires(cfg, m, localcfg.RequiredLastFrame), !n.audio && (requires(cfg, m, localcfg.RequiredReference) || requires(cfg, m, localcfg.RequiredAudio)):
		return notGiven
	}
	return ""
}
