package wfrefresh_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/wfrefresh"
)

const endpoint = "bundled-comfyui"

// fakeModels keeps the saved models of two endpoints and records the updates.
type fakeModels struct {
	saved   []localmodels.ModelView
	updates map[string]localcfg.Model
	err     error
}

func (f *fakeModels) View() (localmodels.View, error) { return localmodels.View{Models: f.saved}, nil }

func (f *fakeModels) UpdateModel(_ context.Context, ep, name string, in localmodels.ModelInput) error {
	if f.err != nil {
		return f.err
	}
	if f.updates == nil {
		f.updates = map[string]localcfg.Model{}
	}
	f.updates[ep+"/"+name] = in.Model
	return nil
}

func template(t *testing.T, id string) localcfg.Model {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok)
	return tpl.Model
}

// oldCopy is the copy of a template as it was saved before the template had reference support: the nodes, the inputs and the
// assignments the reference images added are gone, the links they rewired point back to where they were, and the mode fields
// are empty.
func oldCopy(t *testing.T, id string) localcfg.Model {
	t.Helper()
	m := template(t, id)
	w := *m.Workflow
	var g map[string]struct {
		ClassType string                     `json:"class_type"`
		Inputs    map[string]json.RawMessage `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(w.Graph, &g))
	for nid, n := range g {
		if n.ClassType == "LoadImage" || n.ClassType == "ImageScale" || n.ClassType == "VAEEncode" || n.ClassType == "SplitSigmasDenoise" {
			delete(g, nid)
			continue
		}
		for name := range n.Inputs {
			if strings.HasPrefix(name, "images.image_") {
				delete(n.Inputs, name)
			}
		}
	}
	back := map[string]map[string]string{ // the links the template rewired, as they were
		"txt2img-chroma":        {"747/sigmas": `["751", 0]`, "747/latent_image": `["737", 0]`},
		"txt2img-flux-dev-lora": {"3/latent_image": `["5", 0]`},
	}
	for key, v := range back[id] {
		nid, in, _ := strings.Cut(key, "/")
		g[nid].Inputs[in] = json.RawMessage(v)
	}
	var err error
	w.Graph, err = json.Marshal(g)
	require.NoError(t, err)
	w.Assignments = nil
	for _, a := range m.Workflow.Assignments {
		switch {
		case a.Param == localcfg.ParamReferenceImage, a.Param == localcfg.ParamReferenceDenoise, a.Node == "761":
		default:
			w.Assignments = append(w.Assignments, a)
		}
	}
	w.ReferenceMode, w.NoReference, w.ReferenceDenoiseMin = "", nil, 0
	m.Workflow = &w
	return m
}

// editGraph changes one input of one node of the graph of m.
func editGraph(t *testing.T, m localcfg.Model, node, input string, value any) localcfg.Model {
	t.Helper()
	g, err := localcfg.ParseGraph(m.Workflow.Graph)
	require.NoError(t, err)
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	g[node].Inputs[input] = raw
	w := *m.Workflow
	w.Graph, err = json.Marshal(g)
	require.NoError(t, err)
	m.Workflow = &w
	return m
}

func catalogOf(workflows ...catalog.Workflow) *catalog.Catalog {
	return &catalog.Catalog{Models: []catalog.Model{{ID: "m", Workflows: workflows}}}
}

func view(ep, name string, m localcfg.Model) localmodels.ModelView {
	return localmodels.ModelView{Endpoint: ep, Name: name, Model: m}
}

type warnings struct{ n int }

func (w *warnings) warn(string, ...any) { w.n++ }

// A model installed before its template learned reference images is updated to the template's, with the price the user set.
func TestRunUpdatesAnOldCopyAndKeepsThePrices(t *testing.T) {
	for _, id := range []string{"txt2img-chroma", "txt2img-flux-dev-lora", "txt2img-qwen-image21"} {
		old := oldCopy(t, id)
		assert.Empty(t, old.Workflow.ReferenceMode, id)
		old.CostPerImage = 0.07
		f := &fakeModels{saved: []localmodels.ModelView{view(endpoint, "wf", old)}}
		var w warnings

		wfrefresh.Run(context.Background(), f, catalogOf(catalog.Workflow{Name: "wf", Template: id}), endpoint, w.warn)

		got, ok := f.updates[endpoint+"/wf"]
		require.True(t, ok, id)
		assert.Equal(t, template(t, id).Workflow.ReferenceMode, got.Workflow.ReferenceMode, id)
		assert.True(t, got.ImageInputs(localcfg.KindComfyUI), "the model takes a reference image now: %s", id)
		assert.InDelta(t, 0.07, got.CostPerImage, 1e-9, id)
		assert.Empty(t, localcfg.ValidateModel(endpoint+"/wf", got, map[string]localcfg.Endpoint{endpoint: {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"}}), id)
		assert.Zero(t, w.n, id)
	}
}

// A copy the user edited is left alone: any literal of the graph that differs from the template's (the model file, the steps,
// the cfg, the LoRA) is an edit, whatever else the template grew.
func TestRunLeavesAnEditedCopyAlone(t *testing.T) {
	cases := []struct {
		name, template, node, input string
		value                       any
	}{
		{"unet", "txt2img-chroma", "731", "unet_name", "my-chroma.safetensors"},
		{"steps", "txt2img-chroma", "751", "steps", 40},
		{"cfg", "txt2img-chroma", "694", "cfg", 5},
		{"ckpt", "txt2img-flux-dev-lora", "4", "ckpt_name", "my-flux.safetensors"},
		{"lora", "txt2img-flux-dev-lora", "12", "lora_name", "mine.safetensors"},
		{"qwen steps", "txt2img-qwen-image21", "3", "steps", 8},
		{"qwen unet", "txt2img-qwen-image21", "37", "unet_name", "mine.safetensors"},
	}
	for _, c := range cases {
		f := &fakeModels{saved: []localmodels.ModelView{view(endpoint, "wf", editGraph(t, oldCopy(t, c.template), c.node, c.input, c.value))}}
		wfrefresh.Run(context.Background(), f, catalogOf(catalog.Workflow{Name: "wf", Template: c.template}), endpoint, (&warnings{}).warn)
		assert.Empty(t, f.updates, c.name)
	}
}

// A copy with a node or an input the template does not have, or another assignment or output node, is the user's.
func TestRunLeavesACopyWithOtherStructureAlone(t *testing.T) {
	extraNode := oldCopy(t, "txt2img-chroma")
	g, err := localcfg.ParseGraph(extraNode.Workflow.Graph)
	require.NoError(t, err)
	g["900"] = localcfg.Node{ClassType: "CLIPTextEncode", Inputs: map[string]json.RawMessage{"text": json.RawMessage(`"x"`)}}
	w := *extraNode.Workflow
	w.Graph, _ = json.Marshal(g)
	extraNode.Workflow = &w

	extraInput := editGraph(t, oldCopy(t, "txt2img-chroma"), "731", "mine", 1)

	otherOutput := oldCopy(t, "txt2img-chroma")
	w2 := *otherOutput.Workflow
	w2.OutputNode = "298"
	otherOutput.Workflow = &w2

	for name, m := range map[string]localcfg.Model{"node": extraNode, "input": extraInput, "output": otherOutput} {
		f := &fakeModels{saved: []localmodels.ModelView{view(endpoint, "wf", m)}}
		wfrefresh.Run(context.Background(), f, catalogOf(catalog.Workflow{Name: "wf", Template: "txt2img-chroma"}), endpoint, (&warnings{}).warn)
		assert.Empty(t, f.updates, name)
	}
}

// What needs no refresh is left alone: a copy that is current, a template without reference support, a model of another
// endpoint, a model the catalog does not name.
func TestRunLeavesTheOthersAlone(t *testing.T) {
	f := &fakeModels{saved: []localmodels.ModelView{
		view(endpoint, "current", template(t, "txt2img-chroma")),
		view(endpoint, "plain", template(t, "txt2img-flux-schnell")),
		view("other", "wf", oldCopy(t, "txt2img-chroma")),
		view(endpoint, "mine", oldCopy(t, "txt2img-chroma")),
	}}
	cat := catalogOf(
		catalog.Workflow{Name: "current", Template: "txt2img-chroma"},
		catalog.Workflow{Name: "plain", Template: "txt2img-flux-schnell"},
		catalog.Workflow{Name: "wf", Template: "txt2img-chroma"},
		catalog.Workflow{Name: "not-saved", Template: "txt2img-chroma"},
	)
	wfrefresh.Run(context.Background(), f, cat, endpoint, (&warnings{}).warn)
	assert.Empty(t, f.updates)
}

// A pinned model file is not a failure; any other failure is a warning and the next model is still tried.
func TestRunWarnsOfAFailureButNotOfAPinnedFile(t *testing.T) {
	saved := []localmodels.ModelView{view(endpoint, "a", oldCopy(t, "txt2img-chroma")), view(endpoint, "b", oldCopy(t, "txt2img-chroma"))}
	cat := catalogOf(catalog.Workflow{Name: "a", Template: "txt2img-chroma"}, catalog.Workflow{Name: "b", Template: "txt2img-chroma"})

	pinned := &fakeModels{saved: saved, err: localmodels.ErrPinned}
	var quiet warnings
	wfrefresh.Run(context.Background(), pinned, cat, endpoint, quiet.warn)
	assert.Zero(t, quiet.n)

	failing := &fakeModels{saved: saved, err: errors.New("disk full")}
	var loud warnings
	wfrefresh.Run(context.Background(), failing, cat, endpoint, loud.warn)
	assert.Equal(t, 2, loud.n, "one warning for each model, none stops the others")
}
