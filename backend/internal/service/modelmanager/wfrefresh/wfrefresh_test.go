package wfrefresh_test

import (
	"context"
	"errors"
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

// oldCopy is the copy of a template as it was saved before the template had reference support.
func oldCopy(t *testing.T, id string) localcfg.Model {
	m := template(t, id)
	w := *m.Workflow
	w.ReferenceMode, w.NoReference = "", nil
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
	old := oldCopy(t, "txt2img-chroma")
	old.CostPerImage = 0.07
	f := &fakeModels{saved: []localmodels.ModelView{view(endpoint, "wf", old)}}
	var w warnings

	wfrefresh.Run(context.Background(), f, catalogOf(catalog.Workflow{Name: "wf", Template: "txt2img-chroma"}), endpoint, w.warn)

	got, ok := f.updates[endpoint+"/wf"]
	require.True(t, ok)
	assert.Equal(t, localcfg.ModeImg2Img, got.Workflow.ReferenceMode)
	assert.True(t, got.ImageInputs(localcfg.KindComfyUI), "the model takes a reference image now")
	assert.InDelta(t, 0.07, got.CostPerImage, 1e-9)
	assert.Empty(t, localcfg.ValidateModel(endpoint+"/wf", got, map[string]localcfg.Endpoint{endpoint: {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"}}))
	assert.Zero(t, w.n)
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
