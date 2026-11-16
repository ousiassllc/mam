// Package audiolength is the length of the sound a speech-to-video model makes its clip from (Issue #1153): the clip of a workflow with audio_chunks is as long as the sound,
// so the length is read from the file (package audiofile, in pure Go), refused when it is over gentest.max_audio_seconds or cannot be read, and recorded as the clip's
// length. direct.Video and the callers that run a workflow without it (service/workflows) use it, so that every path says and does the same.
package audiolength

import (
	"fmt"
	"math"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/audiofile"
)

// Of is the length of the sound in seconds, rounded up to 0.1 s (the length a clip is recorded with). It refuses (apperr input_unsupported, 422) a sound whose length cannot
// be read and one longer than limitSeconds.
func Of(data []byte, limitSeconds int) (float64, error) {
	d, err := audiofile.Duration(data)
	if err != nil {
		return 0, apperr.New(apperr.CodeInputUnsupported).WithCause("音声の長さを読めません").WithDetail("audio length unreadable: " + err.Error())
	}
	if d > time.Duration(limitSeconds)*time.Second {
		return 0, apperr.New(apperr.CodeInputUnsupported).
			WithCause(fmt.Sprintf("音声が長すぎます（%.1f 秒。上限は %d 秒）", d.Seconds(), limitSeconds)).
			WithDetail(fmt.Sprintf("audio is %.1f seconds, the limit (gentest.max_audio_seconds) is %d", d.Seconds(), limitSeconds))
	}
	return RoundUp(d.Seconds()), nil
}

// RoundUp rounds seconds up to 0.1 s.
func RoundUp(seconds float64) float64 { return math.Ceil(seconds*10-1e-6) / 10 }

// Apply sets the length of a request to the length of its sound when the model's clip is as long as the sound (caps.AudioLength) and the request has one; any other request is
// returned as it is. The refusals are those of Of, with the limit of cfg.Gentest.
func Apply(cfg modeladapter.Config, caps modeladapter.VideoCapabilities, req modeladapter.VideoRequest) (modeladapter.VideoRequest, error) {
	if !caps.AudioLength || req.Audio == nil {
		return req, nil
	}
	seconds, err := Of(req.Audio.Data, cfg.Gentest.AudioSeconds())
	if err != nil {
		return req, err
	}
	req.DurationSeconds = seconds
	return req, nil
}
