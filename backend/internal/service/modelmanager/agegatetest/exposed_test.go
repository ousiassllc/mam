package agegatetest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/exposed"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/exposedgate"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// A model whose graph reads only a general LoRA is not an adult model, but a request's exposed value of its LoRA name puts a file in at run
// time: with the gate closed, the value that names the adult LoRA is refused (the model's own graph is judged by HidesModel, which cannot
// see the value), and so is a queued result whose value was chosen while the gate was open.
func TestAnExposedValueThatNamesAnAdultFileIsRefusedWhileTheGateIsOff(t *testing.T) {
	r := adultRig(t)
	m, _ := graphReading(t, "other.safetensors", mmtest.CkptA.Name)
	wf := *m.Workflow
	wf.ExposedInputs = []exposed.Input{{Node: "12", Input: "lora_name", Type: exposed.String, Label: "LoRA"}}
	m.Workflow = &wf
	require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: modelmanager.BundledEndpoint, Name: "picks-lora", Model: m}))
	id := localcfg.ID(localcfg.KindComfyUI, modelmanager.BundledEndpoint, "picks-lora")
	request := func(lora string) gentest.Request {
		return gentest.Request{
			Media: gentest.MediaImage, Models: []string{id}, Count: 1, Prompts: []gentest.PromptInput{{Prompt: "a fox"}},
			Exposed: map[string]any{"12.lora_name": lora},
		}
	}
	_, err := r.Gentest.Estimate(ctx(), request(mmtest.LoraD.Name))
	require.NoError(t, err, "the gate is open")

	require.NoError(t, r.Svc.SetAdultContent(false, nil))

	hides := exposedgate.Hides(r.Svc)
	assert.False(t, r.Svc.HidesModel(id), "its own graph reads a general file")
	assert.True(t, hides(id, map[string]any{"12.lora_name": mmtest.LoraD.Name}))
	assert.False(t, hides(id, map[string]any{"12.lora_name": "other2.safetensors"}))

	var invalid *gterr.InvalidError
	_, err = r.Gentest.Estimate(ctx(), request(mmtest.LoraD.Name))
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "成人向け")
	_, err = r.Gentest.Run(ctx(), request(mmtest.LoraD.Name), gentest.Confirmation{})
	require.ErrorAs(t, err, &invalid)

	_, err = r.Gentest.Estimate(ctx(), request("other2.safetensors"))
	assert.NoError(t, err, "a general file is still chosen")
}

// The choices of a LoRA input the model offers (modelcatalog.Info.Exposed, what the screen and list_generation_models show) leave out the adult
// file while the gate is closed, and come back when it opens; the file the graph holds stays.
func TestTheChoicesAModelOffersLeaveOutAdultFilesWhileTheGateIsOff(t *testing.T) {
	r := adultRig(t)
	m, _ := graphReading(t, "other.safetensors", mmtest.CkptA.Name)
	wf := *m.Workflow
	wf.ExposedInputs = []exposed.Input{{Node: "12", Input: "lora_name", Type: exposed.Enum, Label: "LoRA", Options: []string{"other.safetensors", mmtest.LoraD.Name}}}
	m.Workflow = &wf
	require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: modelmanager.BundledEndpoint, Name: "offers-lora", Model: m}))
	id := localcfg.ID(localcfg.KindComfyUI, modelmanager.BundledEndpoint, "offers-lora")
	options := func() []string {
		info, err := r.Gentest.Model(ctx(), gentest.MediaImage, id)
		require.NoError(t, err)
		require.Len(t, info.Exposed, 1)
		return info.Exposed[0].Options
	}
	assert.Equal(t, []string{"other.safetensors", mmtest.LoraD.Name}, options(), "the gate is open")

	require.NoError(t, r.Svc.SetAdultContent(false, nil))
	assert.Equal(t, []string{"other.safetensors"}, options())
}
