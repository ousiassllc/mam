package videocaps

import (
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// CheckAudio refuses (apperr input_unsupported, HTTP 422) the job of a Shot with audio (Issue #1153) for a model that takes no sound file: handing it the job would
// drop the sound without a word and make a clip that ignores it. The Shot's route only holds models that take the sound (modelroute), so this is the last line, not the rule.
func CheckAudio(m domain.ModelName, c Capabilities) error {
	if c.AudioInput {
		return nil
	}
	return apperr.New(apperr.CodeInputUnsupported).
		WithCause(fmt.Sprintf("「%s」は音声ファイルを受け取れないため、音声つきの Shot の動画は作れません。音声を受け取れる動画モデル（Wan 2.2 S2V 14B など）を使うか、Shot の音声を外してください。", m)).
		WithDetail(string(m) + " takes no audio file but the Shot has one")
}

// SoundClip is the length of the clip of a Shot with audio: the length of the sound (sound, seconds, measured by the caller) for a model whose clip is as long as its sound
// (AudioLength), else snapped, the length the model's own lengths gave.
func SoundClip(c Capabilities, snapped, sound float64) float64 {
	if c.AudioLength && sound > 0 {
		return sound
	}
	return snapped
}
