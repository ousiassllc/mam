// Package speedtest runs the speed modes of the generation test (Issue #930: Wan 2.2 14B + LightX2V, 4 steps or the usual 20; Issue #991:
// Qwen-Image-Edit 2511 + Lightning, 4 or 8 steps) over the fake ComfyUI server of the Model Adapter: from the request to the graph the
// server received.
package speedtest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

var ctx = context.Background

const (
	fast  = "comfyui:box/fast"
	plain = "comfyui:box/plain"
)

// box registers the fake ComfyUI with "fast" (Wan 2.2 14B + LightX2V, which has the speed modes) and "plain" (the 5B, which has none).
func box(t *testing.T, r *mmtest.Rig) (*comfyuitest.Fake, string) {
	t.Helper()
	f := comfyuitest.New(t)
	f.OutputFile, f.OutputNode, f.OutputBody = "clip.mp4", "59", []byte("mp4")
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	for name, tpl := range map[string]string{"fast": "img2video-wan22-14b-lightx2v", "plain": "img2video-wan22-5b"} {
		tm, ok := localcfg.TemplateByID(tpl)
		require.True(t, ok)
		require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: name, Model: tm.Model}))
	}
	staged, _, err := r.Gentest.StageReference(comfyuitest.PNG)
	require.NoError(t, err)
	return f, staged
}

func request(staged, mode string, models ...string) gentest.Request {
	return gentest.Request{
		Media: "video", Models: models, Prompts: []gentest.PromptInput{{Prompt: "waves"}}, SpeedMode: mode,
		References: []gentest.RefInput{{Source: gentest.FromStaged, ID: staged, Role: "first_frame"}},
	}
}

func run(t *testing.T, r *mmtest.Rig, req gentest.Request) gentest.BatchStatus {
	t.Helper()
	est, err := r.Gentest.Estimate(ctx(), req)
	require.NoError(t, err)
	ref, err := r.Gentest.Run(ctx(), req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
	require.NoError(t, err)
	r.Start()
	var st gentest.BatchStatus
	require.Eventually(t, func() bool {
		st, err = r.Gentest.Batch(ctx(), ref.BatchID, true)
		return err == nil && st.Finished
	}, 30*time.Second, 20*time.Millisecond, "the batch did not end")
	return st
}

func steps(t *testing.T, f *comfyuitest.Fake) (float64, float64) {
	t.Helper()
	var steps, cfg float64
	g := f.Graph()
	require.NoError(t, json.Unmarshal(g["3"].Inputs["steps"], &steps))
	require.NoError(t, json.Unmarshal(g["3"].Inputs["cfg"], &cfg))
	return steps, cfg
}

// The model list says which models have speed modes and which is the default, so the screen and the MCP tool can offer them.
func TestTheModelListShowsTheSpeedModesAndTheDefault(t *testing.T) {
	r := mmtest.New(t)
	box(t, r)
	for _, m := range r.Gentest.Models(ctx(), "video") {
		switch m.ID {
		case fast:
			assert.Equal(t, "fast", m.SpeedModes[0].ID)
			assert.True(t, m.SpeedModes[0].Default)
			assert.True(t, m.HasSpeedMode("standard"))
			assert.False(t, m.SpeedModes[1].Default)
		case plain:
			assert.Empty(t, m.SpeedModes)
		}
	}
}

// With no speed mode the model runs its default (4 steps, cfg 1), and the result remembers what it was asked.
func TestNoSpeedModeRunsTheDefaultAndStandardRunsTheUsualTwentySteps(t *testing.T) {
	r := mmtest.New(t)
	f, staged := box(t, r)

	st := run(t, r, request(staged, "", fast))
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	s, cfg := steps(t, f)
	assert.Equal(t, [2]float64{4, 1}, [2]float64{s, cfg})
	assert.Empty(t, st.Results[0].SpeedMode)

	staged, _, err := r.Gentest.StageReference(comfyuitest.PNG) // a run uses up its staged picture
	require.NoError(t, err)
	st = run(t, r, request(staged, "standard", fast))
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	s, cfg = steps(t, f)
	assert.Equal(t, [2]float64{20, 3.5}, [2]float64{s, cfg}, "the second run of the same server got the LoRA-free, 20 step graph")
	assert.Equal(t, "standard", st.Results[0].SpeedMode)
}

// Two models, one request: the model that has the mode takes it, the one that has not gets a warning (not a refusal) and runs its default.
func TestAModelWithoutTheModeWarnsAndRunsItsDefault(t *testing.T) {
	r := mmtest.New(t)
	_, staged := box(t, r)
	est, err := r.Gentest.Estimate(ctx(), request(staged, "standard", fast, plain))
	require.NoError(t, err)
	var codes []string
	for _, w := range est.Warnings {
		if w.Code == gentest.WarnSpeedUnsupported {
			codes = append(codes, w.Model)
		}
	}
	assert.Equal(t, []string{plain}, codes)

	est, err = r.Gentest.Estimate(ctx(), request(staged, "", fast, plain))
	require.NoError(t, err)
	for _, w := range est.Warnings {
		assert.NotEqual(t, gentest.WarnSpeedUnsupported, w.Code, "no mode asked, nothing to warn about")
	}
}

func TestASpeedModeIsCheckedBeforeItIsQueued(t *testing.T) {
	r := mmtest.New(t)
	f, staged := box(t, r)
	var invalid *gterr.InvalidError
	_, err := r.Gentest.Estimate(ctx(), request(staged, "Turbo!", fast))
	assert.ErrorAs(t, err, &invalid, "not a name")
	assert.Zero(t, f.Count("POST /prompt"))
}

const edit = "comfyui:pic/edit"

// pic registers Qwen-Image-Edit 2511 (an image model with the Lightning speed modes) and SDXL (an image model with none) at a fake server.
func pic(t *testing.T, r *mmtest.Rig) *comfyuitest.Fake {
	t.Helper()
	f := comfyuitest.New(t)
	f.OutputNode, f.OutputFile, f.OutputBody = "9", "out.png", comfyuitest.PNG
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "pic", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	for name, tpl := range map[string]string{"edit": "txt2img-qwen-image-edit-2511", "sdxl": "txt2img-sdxl"} {
		tm, ok := localcfg.TemplateByID(tpl)
		require.True(t, ok)
		require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "pic", Name: name, Model: tm.Model}))
	}
	return f
}

func imageRequest(mode string, models ...string) gentest.Request {
	return gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "a cat"}}, SpeedMode: mode}
}

// An image model lists its speed modes too (Issue #991), the standard one first and marked as the default.
func TestAnImageModelListsItsSpeedModes(t *testing.T) {
	r := mmtest.New(t)
	pic(t, r)
	seen := false
	for _, m := range r.Gentest.Models(ctx(), "image") {
		switch m.ID {
		case edit:
			seen = true
			require.Len(t, m.SpeedModes, 3)
			assert.Equal(t, "standard", m.SpeedModes[0].ID)
			assert.True(t, m.SpeedModes[0].Default)
			assert.True(t, m.HasSpeedMode("lightning-4"))
			assert.True(t, m.HasSpeedMode("lightning-8"))
			assert.Equal(t, "speed_mode", m.Sampling.Steps.Locked, "the screen disables the steps and the cfg and says why")
		case "comfyui:pic/sdxl":
			assert.Empty(t, m.SpeedModes)
		}
	}
	assert.True(t, seen)
}

// The mode of an image request reaches the graph, and the result remembers it.
func TestAnImageRunTakesTheLightningMode(t *testing.T) {
	r := mmtest.New(t)
	f := pic(t, r)
	st := run(t, r, imageRequest("lightning-4", edit))
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	assert.Equal(t, "lightning-4", st.Results[0].SpeedMode)
	s, cfg := steps(t, f)
	assert.Equal(t, [2]float64{4, 1}, [2]float64{s, cfg})
	assert.Contains(t, string(f.Graph()["90"].Inputs["lora_name"]), "Lightning-4steps")
	assert.NotContains(t, f.Graph(), "91")

	st = run(t, r, imageRequest("", edit))
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	s, cfg = steps(t, f)
	assert.Equal(t, [2]float64{20, 4}, [2]float64{s, cfg}, "no mode asked: the model's default, with no LoRA")
	assert.NotContains(t, f.Graph(), "90")
}

// A model without the mode warns (the same warning as for video) and runs its default; a mode that is not a name is refused.
func TestAnImageModelWithoutTheModeWarnsAndANameThatIsNoneIsRefused(t *testing.T) {
	r := mmtest.New(t)
	f := pic(t, r)
	est, err := r.Gentest.Estimate(ctx(), imageRequest("lightning-4", edit, "comfyui:pic/sdxl"))
	require.NoError(t, err)
	var warned []string
	for _, w := range est.Warnings {
		if w.Code == gentest.WarnSpeedUnsupported {
			warned = append(warned, w.Model)
		}
	}
	assert.Equal(t, []string{"comfyui:pic/sdxl"}, warned)

	var invalid *gterr.InvalidError
	_, err = r.Gentest.Estimate(ctx(), imageRequest("Turbo!", edit))
	assert.ErrorAs(t, err, &invalid)
	assert.Zero(t, f.Count("POST /prompt"))
}
