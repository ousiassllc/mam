// Package audioroutetest tests the video route of a Shot that has a sound file (Issue #1153; spec/architecture/model-adapters.md §3.1). It is a package of its own, so
// that package modelroute stays under the line limit of a directory.
package audioroutetest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute/routetest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

const speech = routetest.Speech

func speechEnv(t *testing.T) modelroute.Env {
	t.Helper()
	return modelroute.Env{Config: routetest.PublishSpeech(t), Check: routetest.NoAPIKey()}
}

func models(cs []modelroute.Candidate) []domain.ModelName {
	var out []domain.ModelName
	for _, c := range cs {
		out = append(out, c.Model)
	}
	return out
}

func assign(env modelroute.Env, m domain.ModelName) modelroute.Env {
	policies := map[domain.QualityPolicy]modeladapter.PolicyRoute{}
	for k, v := range env.Config.Policies {
		policies[k] = v
	}
	policies[domain.QualityBalanced] = modeladapter.PolicyRoute{
		Primary: modeladapter.VideoTarget{Model: m, Resolution: "720p"}, Fallback: modeladapter.VideoTarget{Model: m, Resolution: "720p"}, Takes: 1,
	}
	env.Config.Policies = policies
	return env
}

// A Shot with audio goes to a model that takes it, whatever the Quality Policy's assignment is, and the Take says why.
func TestVideo_AShotWithAudioGoesOnlyToAModelThatTakesSound(t *testing.T) {
	env := speechEnv(t)
	for _, q := range []domain.QualityPolicy{domain.QualityFast, domain.QualityBalanced, domain.QualityMaxQuality} {
		pick, err := env.Video(q, modelroute.VideoNeed{Seconds: 4, Keyframe: true, Audio: true, Reference: true})

		require.NoError(t, err, q)
		assert.Equal(t, speech, pick.Route.Primary.Model, q)
		assert.Equal(t, speech, pick.Route.Fallback.Model, q)
		assert.Equal(t, modelreg.SelectionCondition, pick.Selection, q)
		assert.Contains(t, pick.Reason, "音声", q)
	}
}

// The clip is as long as the sound: the Shot's planned length is not a limit of such a model, and a Keyframe is its picture although it has no first frame.
func TestVideo_ASpeechModelHoldsAShotLongerThanItsListedLengths(t *testing.T) {
	env := speechEnv(t)

	pick, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 30, Keyframe: true, Audio: true, Reference: true})

	require.NoError(t, err)
	assert.Equal(t, speech, pick.Route.Primary.Model)
}

// A Shot without audio keeps the rule it had: a model that cannot run without a sound is never a candidate, with or without a Keyframe.
func TestVideo_AShotWithoutAudioNeverGetsASpeechModel(t *testing.T) {
	env := speechEnv(t)
	for _, keyframe := range []bool{true, false} {
		pick, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Keyframe: keyframe, Reference: keyframe})

		require.NoError(t, err, "keyframe=%v", keyframe)
		assert.NotEqual(t, speech, pick.Route.Primary.Model, "keyframe=%v", keyframe)
		for _, c := range pick.Passed {
			assert.NotEqual(t, speech, c.Model)
		}
	}
}

func TestVideo_AnAssignmentThatNamesASpeechModelIsHonouredForAShotWithAudio(t *testing.T) {
	env := assign(speechEnv(t), speech)

	pick, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Keyframe: true, Audio: true, Reference: true})

	require.NoError(t, err)
	assert.Equal(t, speech, pick.Route.Primary.Model)
	assert.Equal(t, modelreg.SelectionAssignment, pick.Selection, "the assignment was not replaced")
}

// The model needs a picture: a Shot that has neither a Keyframe nor a reference image cannot use it, and the refusal says so.
func TestVideo_AShotWithAudioAndNoPictureIsRefusedWithWhatIsMissing(t *testing.T) {
	env := speechEnv(t)

	_, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Audio: true})

	var nu *modelroute.NoUsableError
	require.ErrorAs(t, err, &nu)
	assert.ErrorIs(t, err, modelroute.ErrNoUsableModel)
	assert.True(t, nu.Audio)
	assert.Empty(t, nu.Candidates)
	var why string
	for _, u := range nu.Unmatched {
		if u.Model == speech {
			why = u.Why
		}
	}
	assert.Contains(t, why, "参照画像が必要")
	assert.Contains(t, why, "Keyframe")
}

// With no model that takes sound installed, the Shot with audio is refused, in words that say what to install or what to take off.
func TestVideo_ShotAudioWithNoSpeechModelSaysWhatIsMissing(t *testing.T) {
	routetest.Publish(t)
	env := modelroute.Env{Config: routetest.LocalConfig(), Check: routetest.NoAPIKey()}

	_, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Keyframe: true, Audio: true, Reference: true})

	var nu *modelroute.NoUsableError
	require.ErrorAs(t, err, &nu)
	assert.True(t, nu.Audio)
	assert.Contains(t, err.Error(), "音声つきの動画")
	assert.Contains(t, err.Error(), "音声を入力に取る動画モデル")
	assert.Contains(t, err.Error(), "音声を外してください")
	var why []string
	for _, u := range nu.Unmatched {
		assert.Equal(t, "音声を使えない", u.Why, u.Model)
		why = append(why, string(u.Model))
	}
	assert.Contains(t, why, string(routetest.LocalVideo), "every usable model is named with the reason")
}

// A Video Model the user fixed, and an assignment the operator pinned, are never replaced: the refusal suggests what would do.
func TestVideo_AFixedOrPinnedModelThatTakesNoSoundIsNotReplacedForAShotWithAudio(t *testing.T) {
	need := modelroute.VideoNeed{Seconds: 4, Keyframe: true, Audio: true, Reference: true}

	t.Run("fixed", func(t *testing.T) {
		need := need
		need.Forced = routetest.LocalVideo
		_, err := speechEnv(t).Video(domain.QualityBalanced, need)

		var nu *modelroute.NoUsableError
		require.ErrorAs(t, err, &nu)
		assert.True(t, nu.Explicit)
		assert.Equal(t, []domain.ModelName{speech}, models(nu.Candidates), "the model that would do is suggested")
	})
	t.Run("fixed to the speech model", func(t *testing.T) {
		need := need
		need.Forced = speech
		pick, err := speechEnv(t).Video(domain.QualityBalanced, need)

		require.NoError(t, err)
		assert.Equal(t, speech, pick.Route.Primary.Model)
	})
	t.Run("pinned", func(t *testing.T) {
		env := speechEnv(t)
		env.Pinned = true
		_, err := env.Video(domain.QualityBalanced, need)

		var nu *modelroute.NoUsableError
		require.ErrorAs(t, err, &nu)
		assert.True(t, nu.Pinned)
		assert.Equal(t, []domain.ModelName{speech}, models(nu.Candidates))
	})
}
