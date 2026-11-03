package editplustest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// The speed modes of Qwen-Image-Edit 2511 (Issue #991): standard is the graph as it stands (20 steps, cfg 4, no LoRA), lightning-8 and
// lightning-4 put the Lightning LoRA of their step count behind the diffusion model and sample with cfg 1.

const (
	lora4 = "Qwen-Image-Edit-2511-Lightning-4steps-V1.0-bf16.safetensors"
	lora8 = "Qwen-Image-Edit-2511-Lightning-8steps-V1.0-bf16.safetensors"
)

func run(t *testing.T, req modeladapter.ImageRequest) submitted {
	t.Helper()
	f := comfyuitest.New(t)
	_, err := backend(t, f, "txt2img-qwen-image-edit-2511").GenerateImage(ctx(t), "", req)
	require.NoError(t, err)
	return f.Graph()
}

func TestNoSpeedModeIsTheStandardOneWithoutAnyLoRANode(t *testing.T) {
	for _, mode := range []string{"", "standard"} {
		g := run(t, modeladapter.ImageRequest{Prompt: "x", AspectRatio: "1:1", Options: localcfg.ImageOptions{SpeedMode: mode}})
		assert.Equal(t, 20.0, num(g["3"].Inputs["steps"]), mode)
		assert.Equal(t, 4.0, num(g["3"].Inputs["cfg"]), mode)
		assert.Equal(t, `"37"/0`, linkTo(g["66"].Inputs["model"]), mode)
		assert.NotContains(t, g, "90", "the server is not asked to load a LoRA the run does not use")
		assert.NotContains(t, g, "91", mode)
	}
}

func TestALightningModeLoadsOnlyItsOwnLoRAAndSetsTheStepsAndTheCFG(t *testing.T) {
	for _, c := range []struct {
		mode, node, other, file string
		steps                   float64
	}{
		{"lightning-4", "90", "91", lora4, 4},
		{"lightning-8", "91", "90", lora8, 8},
	} {
		g := run(t, modeladapter.ImageRequest{Prompt: "x", AspectRatio: "1:1", Options: localcfg.ImageOptions{SpeedMode: c.mode}})
		assert.Equal(t, "LoraLoaderModelOnly", g[c.node].ClassType, c.mode)
		assert.Equal(t, c.file, str(g[c.node].Inputs["lora_name"]), c.mode)
		assert.Equal(t, 1.0, num(g[c.node].Inputs["strength_model"]), c.mode)
		assert.Equal(t, `"37"/0`, linkTo(g[c.node].Inputs["model"]), c.mode)
		assert.Equal(t, `"`+c.node+`"/0`, linkTo(g["66"].Inputs["model"]), c.mode)
		assert.NotContains(t, g, c.other, c.mode)
		assert.Equal(t, c.steps, num(g["3"].Inputs["steps"]), c.mode)
		assert.Equal(t, 1.0, num(g["3"].Inputs["cfg"]), c.mode)
		assert.Equal(t, `"75"/0`, linkTo(g["3"].Inputs["model"]), "the rest of the chain (AuraFlow shift, CFGNorm) is kept")
	}
}

// The mode owns the steps and the cfg: a request's own values (and the references of the run) change nothing about them.
func TestTheSpeedModeWinsOverTheStepsAndTheCFGOfTheRequest(t *testing.T) {
	steps, cfg := 30, 7.0
	req := refs("x", ref("edit", 1))
	req.Options.SpeedMode = "lightning-4"
	req.Options.Sampling = localcfg.SamplingSettings{Steps: &steps, CFG: &cfg}
	g := run(t, req)
	assert.Equal(t, 4.0, num(g["3"].Inputs["steps"]))
	assert.Equal(t, 1.0, num(g["3"].Inputs["cfg"]))
	assert.Equal(t, 1, slotsOf(g, "6"), "the reference still reaches the encoder")
}

// The LoRA of a request goes behind the diffusion model, ahead of the Lightning one: UNETLoader -> request's LoRA -> Lightning -> AuraFlow.
func TestARequestLoRAStacksUnderTheLightningLoRA(t *testing.T) {
	req := modeladapter.ImageRequest{Prompt: "x", AspectRatio: "1:1", Options: localcfg.ImageOptions{SpeedMode: "lightning-8"}}
	req.Options.LoRA = &localcfg.LoRA{Name: "mine.safetensors", Strength: 0.6}
	g := run(t, req)
	var user string
	for id, n := range g {
		if n.ClassType == "LoraLoaderModelOnly" && str(n.Inputs["lora_name"]) == "mine.safetensors" {
			user = id
		}
	}
	require.NotEmpty(t, user, "the request's LoRA was inserted")
	assert.Equal(t, `"37"/0`, linkTo(g[user].Inputs["model"]))
	assert.Equal(t, `"`+user+`"/0`, linkTo(g["91"].Inputs["model"]))
	assert.Equal(t, `"91"/0`, linkTo(g["66"].Inputs["model"]))
}

func TestAnUnknownSpeedModeOfAnImageWorkflowIsRefused(t *testing.T) {
	f := comfyuitest.New(t)
	_, err := backend(t, f, "txt2img-qwen-image-edit-2511").GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x", AspectRatio: "1:1", Options: localcfg.ImageOptions{SpeedMode: "fast"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no speed mode "fast"`)
	assert.Zero(t, f.Count("POST /prompt"))
}
