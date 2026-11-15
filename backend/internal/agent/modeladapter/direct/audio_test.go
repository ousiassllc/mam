package direct_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A sound (Issue #1153) goes only to a model whose workflow takes one: the others refuse it, before anything is sent, rather than drop it without a word.
func TestVideoGivesTheSoundOnlyToAModelThatTakesOne(t *testing.T) {
	wf := func(params ...localcfg.Param) *localcfg.Workflow {
		w := &localcfg.Workflow{}
		for _, p := range params {
			w.Assignments = append(w.Assignments, localcfg.Assignment{Param: p, Node: "1", Input: "x"})
		}
		return w
	}
	video := &localcfg.VideoSettings{Durations: []float64{5}, Resolutions: []string{"720p"}}
	cfg := modeladapter.DefaultConfig()
	cfg.Endpoints = map[string]localcfg.Endpoint{"box": {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}}
	cfg.LocalModels = map[string]localcfg.Model{
		"box/s2v": {Media: localcfg.MediaVideo, Workflow: wf(localcfg.ParamReferenceImage, localcfg.ParamAudio), Video: video},
		"box/i2v": {Media: localcfg.MediaVideo, Workflow: wf(localcfg.ParamFirstFrame), Video: video},
	}
	s2v, i2v := domain.ModelName(localcfg.ID(localcfg.KindComfyUI, "box", "s2v")), domain.ModelName(localcfg.ID(localcfg.KindComfyUI, "box", "i2v"))
	fs2v, fi2v := &fakeVideo{}, &fakeVideo{}
	a := adapter(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{s2v: fs2v, i2v: fi2v}})
	sound := &modeladapter.Input{Data: []byte("RIFF....WAVE"), MimeType: "audio/wav"}
	pic := &modeladapter.Input{Data: []byte("p"), MimeType: "image/png"}

	_, err := direct.Video(t.Context(), a, modeladapter.VideoTarget{Model: i2v, Resolution: "720p"},
		modeladapter.VideoRequest{DurationSeconds: 5, FirstFrame: pic, Audio: sound})
	assert.ErrorIs(t, err, direct.ErrInputsUnsupported, "an image-to-video model has no use for a sound")
	assert.Empty(t, fi2v.reqs, "a refused request is never sent")

	out, err := direct.Video(t.Context(), a, modeladapter.VideoTarget{Model: s2v, Resolution: "720p"},
		modeladapter.VideoRequest{DurationSeconds: 5, References: []modeladapter.Input{*pic}, Audio: sound})
	require.NoError(t, err)
	out.File.Discard()
	require.Len(t, fs2v.reqs, 1)
	assert.Equal(t, sound, fs2v.reqs[0].Audio)

	out, err = direct.Video(t.Context(), a, modeladapter.VideoTarget{Model: s2v, Resolution: "720p"}, modeladapter.VideoRequest{DurationSeconds: 5, References: []modeladapter.Input{*pic}})
	require.NoError(t, err, "direct does not require the sound: the workflow's own check (comfyui missing) does")
	out.File.Discard()
}
