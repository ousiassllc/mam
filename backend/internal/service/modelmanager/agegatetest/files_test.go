package agegatetest_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

func modelsettingsInput(v modelsettings.View) modelsettings.Input {
	return modelsettings.Input{Images: v.Images, Policies: v.Policies}
}

// The files of the test catalog: d.safetensors is the adult LoRA of m-d, a.safetensors the checkpoint it shares with the general m-a.
// The Flux LoRA template reads other file names, so the graphs below are the template with the files of the test catalog.
func graphReading(t *testing.T, lora, ckpt string) (localcfg.Model, []byte) {
	t.Helper()
	tpl, ok := localcfg.TemplateByID("txt2img-flux-dev-lora")
	require.True(t, ok)
	g := bytes.ReplaceAll(tpl.Model.Workflow.Graph, []byte("NSFW_master_Flux.safetensors"), []byte(lora))
	g = bytes.ReplaceAll(g, []byte("flux1-dev-fp8.safetensors"), []byte(ckpt))
	m := tpl.Model
	wf := *m.Workflow
	wf.Graph = g
	m.Workflow = &wf
	return m, g
}

func createModel(t *testing.T, r *mmtest.Rig, endpoint, name, lora string) domain.ModelName {
	t.Helper()
	m, _ := graphReading(t, lora, mmtest.CkptA.Name)
	require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: endpoint, Name: name, Model: m}))
	return domain.ModelName(localcfg.ID(localcfg.KindComfyUI, endpoint, name))
}

// A model that was made under another name, or on another ComfyUI endpoint, or from an imported graph, is judged by the files its
// graph reads: one that reads the adult LoRA is an adult model whatever it is called; one that reads only the base is not.
func TestAModelThatReadsAnAdultFileIsGatedWhateverItIsCalled(t *testing.T) {
	r := adultRig(t)
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "other-comfy", Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}))
	renamed := createModel(t, r, modelmanager.BundledEndpoint, "pretty-picture", mmtest.LoraD.Name)
	elsewhere := createModel(t, r, "other-comfy", "adult-too", mmtest.LoraD.Name)
	plain := createModel(t, r, modelmanager.BundledEndpoint, "just-the-base", "other.safetensors")
	require.NotEqual(t, renamed, adult)
	for _, m := range []domain.ModelName{renamed, elsewhere, plain} {
		assert.False(t, r.Svc.HidesModel(string(m)), "the gate is open")
	}
	view, err := r.Sets.View()
	require.NoError(t, err)
	in := modelsettingsInput(view)
	in.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{Primary: modeladapter.ImageTarget{Model: renamed}, Fallback: modeladapter.ImageTarget{Model: domain.ModelNanoBanana}}
	require.NoError(t, r.Sets.Save(in), "chosen while the gate is open")

	require.NoError(t, r.Svc.SetAdultContent(false, nil))

	assert.True(t, r.Svc.HidesModel(string(renamed)))
	assert.True(t, r.Svc.HidesModel(string(elsewhere)))
	assert.False(t, r.Svc.HidesModel(string(plain)), "a graph that reads no adult file is not adult")
	assert.False(t, r.Svc.HidesModel("comfyui:bundled-comfyui/not-a-model"))
	assert.False(t, r.Svc.HidesModel(string(domain.ModelFlux2)), "a built-in model is never hidden")
	assert.NotContains(t, imageChoices(t, r), renamed)
	assert.NotContains(t, imageChoices(t, r), elsewhere)
	assert.Contains(t, imageChoices(t, r), plain)
	view, err = r.Sets.View()
	require.NoError(t, err)
	assert.Equal(t, []domain.ModelName{renamed}, view.Gated)

	_, err = r.Adapter.GenerateImage(ctx(), modeladapter.PurposePreview, modeladapter.ImageRequest{Prompt: "p"})
	require.ErrorIs(t, err, agegate.ErrRefused)
	_, err = direct.Image(ctx(), r.Adapter, elsewhere, modeladapter.ImageRequest{Prompt: "p"})
	require.ErrorIs(t, err, agegate.ErrRefused)
	assert.Equal(t, proc.StateStopped, r.Svc.Engine().Process.State, "no call reached ComfyUI")

	require.NoError(t, r.Svc.SetAdultContent(true, nil))
	assert.False(t, r.Svc.HidesModel(string(renamed)))
}

// A saved workflow ("comfy:<id>@v<N>", an imported graph) is judged the same way, and running it is refused while the gate is
// closed: StartRun calls the backend itself, so it asks the gate.
func TestASavedWorkflowThatReadsAnAdultFileIsGatedAndItsRunIsRefused(t *testing.T) {
	r := adultRig(t)
	svc := workflows.New(workflows.Deps{Repo: comfywf.New(r.DB()), Models: r.Models, Adapter: r.Adapter, Keys: r.Keys, StagingDir: t.TempDir(), RunsDir: t.TempDir()})
	t.Cleanup(func() { localcfg.PublishWorkflows(nil) })
	adultModel, adultGraph := graphReading(t, mmtest.LoraD.Name, mmtest.CkptA.Name)
	_, plainGraph := graphReading(t, "other.safetensors", mmtest.CkptA.Name)
	spec := localcfg.WorkflowSpec{Assignments: adultModel.Workflow.Assignments, OutputNode: adultModel.Workflow.OutputNode}
	save := func(name string, graph []byte) workflows.Workflow {
		w, err := svc.Create(ctx(), workflows.CreateInput{Name: name, Kind: localcfg.MediaImage, API: graph, Spec: &spec})
		require.NoError(t, err)
		require.True(t, w.Ready, w.Problems)
		return w
	}
	imported, general := save("imported", adultGraph), save("general", plainGraph)

	require.NoError(t, r.Svc.SetAdultContent(false, nil))

	assert.True(t, r.Svc.HidesModel(imported.ModelID))
	assert.False(t, r.Svc.HidesModel(general.ModelID))
	_, err := svc.StartRun(ctx(), workflows.RunInput{WorkflowID: imported.ID, Prompt: "p"})
	var invalid *workflows.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "成人向け")
	assert.Contains(t, invalid.Message, imported.ModelID)
	_, err = direct.Image(ctx(), r.Adapter, domain.ModelName(imported.ModelID), modeladapter.ImageRequest{Prompt: "p"})
	require.ErrorIs(t, err, agegate.ErrRefused, "a Shot's Video Model lock or an assignment that names the workflow is refused as well")
	assert.Equal(t, proc.StateStopped, r.Svc.Engine().Process.State, "no call reached ComfyUI")
}
