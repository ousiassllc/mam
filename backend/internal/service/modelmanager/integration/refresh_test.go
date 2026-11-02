package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// storedModel is the saved model of the managed endpoint called name.
func (r *rig) storedModel(name string) localcfg.Model {
	r.t.Helper()
	v, err := r.models.View()
	require.NoError(r.t, err)
	for _, m := range v.Models {
		if m.Endpoint == modelmanager.BundledEndpoint && m.Name == name {
			return m.Model
		}
	}
	r.t.Fatalf("no model %s on the managed endpoint", name)
	return localcfg.Model{}
}

// withChromaTemplate makes the catalog's first model use the Chroma template, which takes a reference image.
func withChromaTemplate(d *modelmanager.Deps, _ *rig) {
	d.Catalog.Models[0].Workflows = d.Catalog.Models[0].Workflows[:1]
	d.Catalog.Models[0].Workflows[0].Template = "txt2img-chroma"
}

// A model installed before its template learned reference images keeps the old copy; the start of mam brings it up to the
// template and keeps the price the user set (#933).
func TestStartUpGivesAnInstalledModelTheReferenceImageSupportOfItsTemplate(t *testing.T) {
	r := newRig(t, withChromaTemplate)
	r.installEngine()
	require.Equal(t, "succeeded", string(r.installModel("m-a").State))

	old := r.storedModel("wf-a")
	old.Workflow.ReferenceMode, old.Workflow.NoReference = "", nil // the copy of before #933
	old.CostPerImage = 0.07
	require.NoError(t, r.models.UpdateModel(ctx(), modelmanager.BundledEndpoint, "wf-a", localmodels.ModelInput{Model: old}))
	require.Empty(t, r.storedModel("wf-a").Workflow.ReferenceMode)

	r.svc.OnStartup(ctx())

	got := r.storedModel("wf-a")
	assert.Equal(t, localcfg.ModeImg2Img, got.Workflow.ReferenceMode)
	assert.True(t, got.ImageInputs(localcfg.KindComfyUI), "the model takes a reference image now")
	assert.InDelta(t, 0.07, got.CostPerImage, 1e-9, "the price the user set is kept")
}

// A model whose template has no reference support, and one that already has it, are left as the user has them.
func TestStartUpLeavesModelsThatNeedNoRefreshAlone(t *testing.T) {
	r := newRig(t, withChromaTemplate)
	r.installEngine()
	require.Equal(t, "succeeded", string(r.installModel("m-a").State))
	require.NoError(t, r.svc.AcceptLicense("m-c", modelmanager.LicenseRef{ID: "non-commercial", Version: "1"}))
	require.Equal(t, "succeeded", string(r.installModel("m-c").State), "txt2img-flux-schnell: no reference mode")

	edited := r.storedModel("wf-a")
	edited.CostPerImage = 0.5 // already current: the user's edit stays
	require.NoError(t, r.models.UpdateModel(ctx(), modelmanager.BundledEndpoint, "wf-a", localmodels.ModelInput{Model: edited}))
	plain := r.storedModel("wf-c")
	plain.CostPerImage = 0.2
	require.NoError(t, r.models.UpdateModel(ctx(), modelmanager.BundledEndpoint, "wf-c", localmodels.ModelInput{Model: plain}))

	r.svc.OnStartup(ctx())

	assert.InDelta(t, 0.5, r.storedModel("wf-a").CostPerImage, 1e-9)
	assert.InDelta(t, 0.2, r.storedModel("wf-c").CostPerImage, 1e-9)
	assert.Empty(t, r.storedModel("wf-c").Workflow.ReferenceMode)
}
