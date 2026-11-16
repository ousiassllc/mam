package modelroute_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

// speechModel is a local workflow that makes the clip from a sound (Issue #1153). It takes the first frame as well and is named so that it sorts before the
// other local video model: were it not left out, it would be the one picked.
const speechModel domain.ModelName = "comfyui:bundled-comfyui/a-s2v"

func withSpeechModel(t *testing.T) modelroute.Env {
	t.Helper()
	cfg := localConfig()
	cfg.LocalModels = maps.Clone(cfg.LocalModels)
	cfg.LocalModels["bundled-comfyui/a-s2v"] = localcfg.Model{
		Media: localcfg.MediaVideo, Video: &localcfg.VideoSettings{Durations: []float64{4, 5}, Resolutions: []string{"480p", "720p"}},
		Workflow: &localcfg.Workflow{Assignments: []localcfg.Assignment{{Param: localcfg.ParamFirstFrame}, {Param: localcfg.ParamAudio}}},
	}
	modeladapter.PublishLocal(cfg.Endpoints, cfg.LocalModels, cfg.Workflows)
	t.Cleanup(func() { modeladapter.PublishLocal(nil, nil, nil) })
	modelreg.Set(nil, []string{string(speechModel), string(localVideo), string(localVideo2)})
	t.Cleanup(func() { modelreg.Set(nil, nil) })
	return modelroute.Env{Config: cfg, Check: noAPIKey()}
}

// A Project's Shot gives a model a prompt, a Keyframe and a length, never a sound file, so a model that cannot run without one is not a candidate for it.
func TestVideo_AModelThatRequiresASoundIsNeverPickedForAShot(t *testing.T) {
	env := withSpeechModel(t)
	for _, keyframe := range []bool{true, false} {
		pick, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Keyframe: keyframe})
		require.NoError(t, err)
		assert.NotEqual(t, speechModel, pick.Route.Primary.Model, "keyframe=%v", keyframe)
		assert.NotEqual(t, speechModel, pick.Route.Fallback.Model, "keyframe=%v", keyframe)
		for _, c := range pick.Passed {
			assert.NotEqual(t, speechModel, c.Model)
		}
	}
}

func TestVideo_TheReasonTheSoundModelDoesNotMatchIsTheSound(t *testing.T) {
	env := withSpeechModel(t)
	env.Pinned = true // nothing is replaced: the refusal says which models were left out and why
	modelreg.Set(nil, []string{string(speechModel)})

	_, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Keyframe: true})

	var nu *modelroute.NoUsableError
	require.ErrorAs(t, err, &nu)
	assert.Empty(t, nu.Candidates)
	require.Len(t, nu.Unmatched, 1)
	assert.Equal(t, speechModel, nu.Unmatched[0].Model)
	assert.Contains(t, nu.Unmatched[0].Why, "音声")
}
