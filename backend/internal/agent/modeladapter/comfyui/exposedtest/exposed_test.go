// Package exposedtest pins how the exposed inputs of a workflow (Issue #977, localcfg/exposed) reach the graph the ComfyUI server gets.
package exposedtest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/exposed"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

func ptr[T any](v T) *T { return &v }

// sdxl is the SDXL template's workflow with the exposed inputs put in (nil: as it ships).
func sdxl(t *testing.T, inputs ...exposed.Input) localcfg.Workflow {
	t.Helper()
	tp, ok := localcfg.TemplateByID("txt2img-sdxl")
	require.True(t, ok)
	w := *tp.Model.Workflow
	w.ExposedInputs = inputs
	return w
}

func denoise() exposed.Input {
	return exposed.Input{Node: "3", Input: "denoise", Type: exposed.Float, Label: "ノイズ", Min: ptr(0.0), Max: ptr(1.0)}
}

func batch() exposed.Input {
	return exposed.Input{Node: "5", Input: "batch_size", Type: exposed.Int, Min: ptr(1.0), Max: ptr(4.0)}
}

// submitted is the graph the fake server got for one request of the workflow.
func submitted(t *testing.T, w localcfg.Workflow, values map[string]any) map[string]map[string]json.RawMessage {
	t.Helper()
	f := comfyuitest.New(t)
	f.OutputNode, f.OutputFile, f.OutputBody = w.OutputNode, "out.png", comfyuitest.PNG
	c, err := localhttp.New("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: f.URL()}, credentialtest.New(), t.TempDir())
	require.NoError(t, err)
	c.PollEvery = 10 * time.Millisecond
	b := &comfyui.Backend{Client: c, Model: localcfg.Model{Media: localcfg.MediaImage, Workflow: &w}}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	seed := int64(7)
	_, err = b.GenerateImage(ctx, "", modeladapter.ImageRequest{Prompt: "a fox", AspectRatio: "1:1", Seed: &seed, Options: localcfg.ImageOptions{Exposed: values}})
	require.NoError(t, err)
	raw, err := json.Marshal(f.Graph())
	require.NoError(t, err)
	var nodes map[string]struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(raw, &nodes))
	out := map[string]map[string]json.RawMessage{}
	for id, n := range nodes {
		out[id] = n.Inputs
	}
	return out
}

// A workflow without exposed inputs, and one whose exposed inputs are left alone, send the graph the template always sent.
func TestWithoutValuesTheGraphIsTheOneThatShips(t *testing.T) {
	plain := submitted(t, sdxl(t), nil)
	assert.Equal(t, plain, submitted(t, sdxl(t, denoise(), batch()), nil))
	assert.Equal(t, json.RawMessage("1"), plain["3"]["denoise"])
}

func TestTheValuesOfExposedInputsReachTheirNodesAndNothingElse(t *testing.T) {
	plain := submitted(t, sdxl(t), nil)
	got := submitted(t, sdxl(t, denoise(), batch()), map[string]any{"3.denoise": 0.6, "5.batch_size": int64(3)})
	assert.Equal(t, json.RawMessage("0.6"), got["3"]["denoise"])
	assert.Equal(t, json.RawMessage("3"), got["5"]["batch_size"])
	got["3"]["denoise"], got["5"]["batch_size"] = plain["3"]["denoise"], plain["5"]["batch_size"]
	assert.Equal(t, plain, got, "every other input is as it was")
}

func TestAValueOutOfRangeNeverReachesTheServer(t *testing.T) {
	w := sdxl(t, denoise())
	_, err := w.GraphWith(map[string]any{"3.denoise": 1.5})
	assert.Error(t, err)
	_, err = w.GraphWith(map[string]any{"3.steps": 3})
	assert.Error(t, err, "an input that is not exposed")
}

func TestAnExposedWorkflowValidates(t *testing.T) {
	ok := sdxl(t, denoise(), batch())
	assert.Empty(t, localcfg.ValidateSaved(localcfg.Model{Media: localcfg.MediaImage, Workflow: &ok}))
	assert.Len(t, ok.ExposedCaps(), 2)
	assert.InDelta(t, 1.0, ok.ExposedCaps()[0].Default, 0)
	assigned := sdxl(t, exposed.Input{Node: "3", Input: "steps", Type: exposed.Int, Min: ptr(1.0), Max: ptr(150.0)})
	assert.NotEmpty(t, localcfg.ValidateSaved(localcfg.Model{Media: localcfg.MediaImage, Workflow: &assigned}), "an input an assignment feeds")
}

// "今の設定を ComfyUI で開く": the graph that is handed over is the one a run would send, with the values of the request in it.
func TestGraphForIsTheGraphARunSends(t *testing.T) {
	w := sdxl(t, denoise())
	seed := int64(7)
	steps := 33
	req := modeladapter.ImageRequest{
		Prompt: "a fox", AspectRatio: "1:1", Seed: &seed,
		Options: localcfg.ImageOptions{Exposed: map[string]any{"3.denoise": 0.6}, Sampling: localcfg.SamplingSettings{Steps: &steps}},
	}
	raw, err := comfyui.GraphFor(w, req)
	require.NoError(t, err)
	var got map[string]struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, json.RawMessage(`"a fox"`), got["6"].Inputs["text"])
	assert.Equal(t, json.RawMessage("7"), got["3"].Inputs["seed"])
	assert.Equal(t, json.RawMessage("33"), got["3"].Inputs["steps"])
	assert.Equal(t, json.RawMessage("0.6"), got["3"].Inputs["denoise"])
	assert.Equal(t, json.RawMessage("1024"), got["5"].Inputs["width"])
	_, err = comfyui.GraphFor(w, modeladapter.ImageRequest{Prompt: "x", Options: localcfg.ImageOptions{Exposed: map[string]any{"3.denoise": 9.0}}})
	assert.Error(t, err)
}
