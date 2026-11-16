package routetest

import (
	"maps"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/audiochunks"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Speech is a local speech-to-video workflow shaped like Wan 2.2 S2V (Issue #1153): it cannot run without a sound file and a reference image, it has no first frame, and its
// clip is as long as the sound. It sorts before LocalVideo, so that were it not left out of a Shot without audio, it would be the one picked.
const Speech domain.ModelName = "comfyui:bundled-comfyui/a-s2v"

// SpeechConfig is LocalConfig with the Speech model added.
func SpeechConfig() modeladapter.Config {
	cfg := LocalConfig()
	cfg.LocalModels = maps.Clone(cfg.LocalModels)
	cfg.LocalModels["bundled-comfyui/a-s2v"] = localcfg.Model{
		Media: localcfg.MediaVideo, Video: &localcfg.VideoSettings{Durations: []float64{4}, Resolutions: []string{"480p", "720p"}},
		Workflow: &localcfg.Workflow{
			Assignments: []localcfg.Assignment{{Param: localcfg.ParamReferenceImage}, {Param: localcfg.ParamAudio}},
			AudioChunks: &audiochunks.Spec{},
		},
	}
	return cfg
}

// PublishSpeech is Publish with the Speech model among the video models; it returns the configuration that defines it.
func PublishSpeech(t *testing.T) modeladapter.Config {
	t.Helper()
	cfg := SpeechConfig()
	publish(t, cfg, Speech, LocalVideo, LocalVideo2)
	return cfg
}
