package availabilitytest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

const (
	qwenModelID    = "m-qwen"
	qwenWorkflowID = "comfyui:" + modelmanager.BundledEndpoint + "/wf-qwen"
	lightning4     = "Qwen-Image-Edit-2511-Lightning-4steps-V1.0-bf16.safetensors"
	lightning8     = "Qwen-Image-Edit-2511-Lightning-8steps-V1.0-bf16.safetensors"
)

// withQwenModel adds m-qwen: the real workflow of Qwen-Image-Edit 2511 (its speed modes load the two Lightning LoRAs), with the LoRAs as optional files that the
// host does not serve (a mirror that is gone, the machine offline) and two required files.
func withQwenModel() mmtest.Option {
	return func(d *modelmanager.Deps, r *mmtest.Rig) {
		src := d.Catalog.Models[0]
		lora := func(name string) catalog.File {
			f := r.Host.CatalogFile(mmtest.LoraD, true)
			f.Name, f.URL = name, r.Host.URL+"/loras/"+name
			return f
		}
		d.Catalog.Models = append(d.Catalog.Models, catalog.Model{
			ID: qwenModelID, Name: "Qwen", Kind: localcfg.MediaImage, Tier: catalog.TierStandard, Description: "d", VRAM: src.VRAM, License: src.License,
			Files:     []catalog.File{r.Host.CatalogFile(mmtest.ClipC, false), r.Host.CatalogFile(mmtest.VaeBC, false), lora(lightning4), lora(lightning8)},
			Workflows: []catalog.Workflow{{Name: "wf-qwen", Template: "txt2img-qwen-image-edit-2511"}},
		})
	}
}

func qwenInfo(t *testing.T, r *mmtest.Rig) modelcatalog.Info {
	t.Helper()
	for _, m := range r.Gentest.Models(context.Background(), "image") {
		if m.ID == qwenWorkflowID {
			return m
		}
	}
	t.Fatal("the generation test does not list the model")
	return modelcatalog.Info{}
}

func modeIDs(i modelcatalog.Info) []string {
	var out []string
	for _, m := range i.SpeedModes {
		out = append(out, m.ID)
	}
	return out
}

// #1025: the model is installed without its Lightning LoRAs (they could not be fetched). It is usable, but the speed modes that load a LoRA are not offered, with
// the reason, and a request that names one is warned (the estimate and the run use the model's default) instead of reaching ComfyUI to fail on a missing file.
// The mode that leaves the LoRA nodes out (standard) works.
func TestTheSpeedModesThatNeedAMissingLoRAAreNotOffered(t *testing.T) {
	r := mmtest.New(t, withQwenModel())
	r.Start()
	r.InstallEngine()
	r.Host.Gone(mmtest.LoraD)
	st := r.InstallModel(qwenModelID)
	require.Equal(t, "succeeded", string(st.State), st.Error)

	info := qwenInfo(t, r)
	assert.True(t, info.Available, "installed without the optional files")
	assert.Equal(t, []string{"standard"}, modeIDs(info))
	assert.True(t, info.HasSpeedMode("standard"))
	assert.False(t, info.HasSpeedMode("lightning-4"))
	require.Len(t, info.BlockedSpeedModes, 2)
	reason, blocked := info.SpeedModeBlock("lightning-4")
	require.True(t, blocked)
	assert.Contains(t, reason, lightning4, "the 4-step mode names the file it loads")
	assert.NotContains(t, reason, lightning8, "and not the other one")
	reason, _ = info.SpeedModeBlock("lightning-8")
	assert.Contains(t, reason, lightning8)
	assert.Contains(t, reason, "任意ファイルを取得", "and says how to get it")

	// the one judgement is the plan's: the request for the missing mode is warned about, with the reason
	est, err := r.Gentest.Estimate(context.Background(), gentest.Request{Media: "image", Models: []string{qwenWorkflowID}, Prompts: []gentest.PromptInput{{Prompt: "a cat"}}, SpeedMode: "lightning-4"})
	require.NoError(t, err)
	var warned string
	for _, w := range est.Warnings {
		if w.Code == "speed_mode_unsupported" {
			warned = w.Message
		}
	}
	assert.Contains(t, warned, lightning4)
	assert.Contains(t, warned, "既定で作ります")
}

// With a LoRA there only the mode that loads the other one is left out; with both, every mode is offered.
func TestAModeIsOfferedAgainOnceItsLoRAIsThere(t *testing.T) {
	r := mmtest.New(t, withQwenModel())
	r.Start()
	r.InstallEngine()
	r.Host.Serve(mmtest.File{Folder: "loras", Name: lightning8, Body: mmtest.LoraD.Body})
	require.Equal(t, "succeeded", string(r.InstallModel(qwenModelID).State))

	info := qwenInfo(t, r)
	assert.Equal(t, []string{"standard", "lightning-8"}, modeIDs(info), "the 8-step LoRA was fetched, the 4-step one was not served")
	require.Len(t, info.BlockedSpeedModes, 1)
	assert.Equal(t, "lightning-4", info.BlockedSpeedModes[0].ID)
}
