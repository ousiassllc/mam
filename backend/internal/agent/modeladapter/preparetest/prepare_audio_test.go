package preparetest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute/routetest"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

var sound = &modeladapter.Input{Data: []byte("RIFF"), MimeType: "audio/wav"}

func speechJob() modeladapter.VideoJob {
	job := fullJob()
	job.Audio, job.AudioSeconds = sound, 9.4
	job.LastFrame, job.References = nil, nil
	return job
}

// A model of a sound has no first frame: the Shot's Keyframe is its one picture, and the clip is as long as the sound, not as the Shot.
func TestPrepareForASpeechModelGivesTheSoundTheKeyframeAndTheSoundsLength(t *testing.T) {
	routetest.PublishSpeech(t)

	req, err := modeladapter.Prepare(modeladapter.VideoTarget{Model: routetest.Speech, Resolution: "480p"}, speechJob())

	require.NoError(t, err)
	assert.Equal(t, sound, req.Audio)
	assert.Nil(t, req.FirstFrame)
	require.Len(t, req.References, 1)
	assert.Equal(t, []byte("first"), req.References[0].Data, "the Keyframe")
	assert.InDelta(t, 9.4, req.DurationSeconds, 1e-9, "the length of the sound, not the Shot's 4 seconds")
}

// A reference of the Pack is the picture when the Shot has one: the Keyframe is not added to it.
func TestPrepareForASpeechModelKeepsThePackReferenceAsThePicture(t *testing.T) {
	routetest.PublishSpeech(t)
	job := speechJob()
	job.References = []modeladapter.Input{{Data: []byte("character"), MimeType: "image/png"}}

	req, err := modeladapter.Prepare(modeladapter.VideoTarget{Model: routetest.Speech, Resolution: "480p"}, job)

	require.NoError(t, err)
	require.Len(t, req.References, 1)
	assert.Equal(t, []byte("character"), req.References[0].Data)
}

// A model that takes no sound is never handed a job with one: the sound would be dropped and the clip would ignore it.
func TestPrepareRefusesASoundForAModelThatTakesNone(t *testing.T) {
	routetest.PublishSpeech(t)

	_, err := modeladapter.Prepare(modeladapter.VideoTarget{Model: routetest.LocalVideo, Resolution: "480p"}, speechJob())
	_, apiErr := modeladapter.Prepare(modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "720p"}, speechJob())

	for _, e := range []error{err, apiErr} {
		var ae *apperr.Error
		require.ErrorAs(t, e, &ae)
		assert.Equal(t, apperr.CodeInputUnsupported, ae.Code)
		assert.Contains(t, ae.Cause, "音声ファイルを受け取れない")
	}
}

// A Shot without audio is prepared as it was: no sound, the frames as before.
func TestPrepareWithoutASoundIsAsBefore(t *testing.T) {
	routetest.PublishSpeech(t)
	job := fullJob()

	req, err := modeladapter.Prepare(modeladapter.VideoTarget{Model: routetest.LocalVideo, Resolution: "480p"}, job)

	require.NoError(t, err)
	assert.Nil(t, req.Audio)
	assert.NotNil(t, req.FirstFrame)
}
