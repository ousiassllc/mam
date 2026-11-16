package workflowstest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// #1153: a Project can hold an audio asset, but the image inputs of a saved workflow take pictures: an audio asset named as a reference is refused before ComfyUI
// is asked (the sound goes in the audio field of the run).
func TestRunRefusesAnAudioAssetAsAReferencePicture(t *testing.T) {
	r, w, _ := s2v(t)
	r.svc.LoadAsset = func(context.Context, string, string) ([]byte, string, error) {
		return audiofiletest.WAV(16000, 16000), "audio/wav", nil
	}
	var invalid *workflows.InvalidError
	_, err := r.svc.StartRun(context.Background(), workflows.RunInput{
		WorkflowID: w.ID, Prompt: "x", Audio: audiofiletest.WAV(16000, 16000), References: []workflows.AssetRef{{ProjectID: "p", AssetID: "voice"}},
	})
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "画像の入力に使えません")
	assert.Empty(t, r.comfy.Uploads(), "nothing reached ComfyUI")
}
