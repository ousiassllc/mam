package workflowstest_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// s2v saves the speech-to-video template (Issue #1153) as a workflow of the service, over a fake ComfyUI whose output is the template's video node. The picture of
// the speaker comes from a Project asset, which the rig has none of: the loader returns a PNG.
func s2v(t *testing.T) (*rig, workflows.Workflow, localcfg.WorkflowSpec) {
	t.Helper()
	r := newRig(t)
	r.comfy.OutputFile, r.comfy.OutputNode, r.comfy.OutputBody = "clip.mp4", "113", []byte("mp4")
	r.svc.LoadAsset = func(context.Context, string, string) ([]byte, string, error) {
		return comfyuitest.PNG, "image/png", nil
	}
	graph, spec := template(t, "audio2video-wan22-s2v-14b")
	w, err := r.svc.Create(context.Background(), workflows.CreateInput{Name: "Talk", Kind: localcfg.MediaVideo, API: graph, Spec: &spec})
	require.NoError(t, err)
	return r, w, spec
}

func assignmentOf(t *testing.T, spec localcfg.WorkflowSpec, p localcfg.Param) localcfg.Assignment {
	t.Helper()
	for _, a := range spec.Assignments {
		if a.Param == p {
			return a
		}
	}
	require.FailNow(t, "no assignment of "+string(p))
	return localcfg.Assignment{}
}

// A saved workflow with an audio assignment runs with a sound file: the bytes are uploaded under the extension of their kind and the name goes where the assignment says.
func TestRunGivesASavedSpeechToVideoWorkflowItsSound(t *testing.T) {
	for _, c := range []struct {
		name, ext string
		data      []byte
	}{
		{"wav", ".wav", audiofiletest.WAV(16000, 48000)},
		{"mp3", ".mp3", audiofiletest.MP3(true, 40)},
		{"flac", ".flac", audiofiletest.FLAC(44100, 44100)},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, w, spec := s2v(t)
			rec, err := r.svc.StartRun(context.Background(), workflows.RunInput{
				WorkflowID: w.ID, Prompt: "she greets us", Audio: c.data, References: []workflows.AssetRef{{ProjectID: "p", AssetID: "a"}},
			})
			require.NoError(t, err)
			done := wait(t, r, rec.ID)
			require.Equal(t, workflows.RunSucceeded, done.Status, done.Error)

			a := assignmentOf(t, spec, localcfg.ParamAudio)
			var name string
			require.NoError(t, json.Unmarshal(r.comfy.Graph()[a.Node].Inputs[a.Input], &name))
			assert.True(t, strings.HasSuffix(name, c.ext), name)
			assert.Contains(t, r.comfy.Uploads(), name, "the graph reads the name the sound was uploaded as")
			assert.Contains(t, r.comfy.UploadBodies(), c.data)
		})
	}
}

// A workflow that needs a sound is not started without one, a workflow that takes none is not given one, and bytes that are no audio are refused: nothing is dropped
// without a word, and the run is refused before ComfyUI is asked.
func TestRunRefusesASoundTheWorkflowCannotUseAndTheLackOfOneItNeeds(t *testing.T) {
	r, w, _ := s2v(t)
	ctx := context.Background()
	sound := audiofiletest.WAV(16000, 16000)
	refs := []workflows.AssetRef{{ProjectID: "p", AssetID: "a"}}
	var invalid *workflows.InvalidError

	_, err := r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "x", References: refs})
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "音声ファイルが要ります")

	_, err = r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "x", References: refs, Audio: []byte("not a sound at all")})
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "WAV・MP3・FLAC・OGG・M4A")

	graph, spec := template(t, "txt2video-wan22-5b")
	clip, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Clip", Kind: localcfg.MediaVideo, API: graph, Spec: &spec})
	require.NoError(t, err)
	_, err = r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: clip.ID, Prompt: "x", Audio: sound})
	require.ErrorAs(t, err, &invalid, "a video workflow without an audio assignment")
	assert.Contains(t, invalid.Message, "音声ファイルを使いません")

	graph, spec = template(t, "txt2img-sdxl")
	still, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Still", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)
	_, err = r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: still.ID, Prompt: "x", Audio: sound})
	require.ErrorAs(t, err, &invalid, "an image workflow")
	assert.Contains(t, invalid.Message, "音声ファイルを使いません")
	assert.Empty(t, r.comfy.Uploads(), "no refusal reached ComfyUI")
}

// Suggest assigns the LoadAudio node of an imported video graph, so that an imported speech-to-video workflow carries the audio assignment without the user finding it.
func TestSuggestAssignsTheLoadAudioNodeOfAVideoGraph(t *testing.T) {
	graph := []byte(`{"1":{"class_type":"LoadAudio","inputs":{"audio":"a.wav"}},"2":{"class_type":"SaveVideo","inputs":{}}}`)
	got := workflows.Suggest(graph, localcfg.MediaVideo)
	assert.Equal(t, []localcfg.Assignment{{Param: localcfg.ParamAudio, Node: "1", Input: "audio"}}, got.Assignments)
	assert.Empty(t, workflows.Suggest(graph, localcfg.MediaImage).Assignments, "an image workflow takes no sound")
}
