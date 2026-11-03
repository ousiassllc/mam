// Package addtest runs wfrefresh.Refresh over the model manager's rig: a model installed before the catalog got a workflow gets the workflow
// at the next start, a copy of that name the user changed stays, and the delete of the model removes the new workflow with the others (#990).
package addtest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/wfrefresh"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

var ctx = context.Background

func names(t *testing.T, r *mmtest.Rig) map[string]localmodels.ModelView {
	t.Helper()
	view, err := r.Models.View()
	require.NoError(t, err)
	out := map[string]localmodels.ModelView{}
	for _, m := range view.Models {
		out[m.Name] = m
	}
	return out
}

func TestAWorkflowTheCatalogGotAfterTheInstallIsAddedAtTheStartAndDeletedWithTheModel(t *testing.T) {
	var cat *catalog.Catalog
	r := mmtest.New(t, func(d *modelmanager.Deps, _ *mmtest.Rig) { cat = d.Catalog })
	r.InstallEngine()
	require.Equal(t, domain.JobSucceeded, r.InstallModel("m-a").State)
	require.NotContains(t, names(t, r), "wf-a-inpaint")

	// the next release of the catalog: m-a got an inpaint workflow (as sdxl-base got sdxl-base-inpaint in #974)
	require.Equal(t, "m-a", cat.Models[0].ID)
	cat.Models[0].Workflows = append(cat.Models[0].Workflows, catalog.Workflow{Name: "wf-a-inpaint", Template: "inpaint-sdxl"})
	var warned int
	warn := func(string, ...any) { warned++ }

	wfrefresh.Refresh(ctx(), r.Models, r.Svc, cat, modelmanager.BundledEndpoint, warn)

	added, ok := names(t, r)["wf-a-inpaint"]
	require.True(t, ok, "the model shows in the list without being installed again")
	assert.True(t, added.ImageInputs("comfyui"), "it is the inpaint template: it takes the source image and the mask")
	assert.Contains(t, r.Svc.InstalledModels()["m-a"].Workflows, "wf-a-inpaint")

	// the user changed the copy: the next start leaves it as it is, and does not record the name twice
	edited := added.Model
	edited.CostPerImage = 0.25
	require.NoError(t, r.Models.UpdateModel(ctx(), modelmanager.BundledEndpoint, "wf-a-inpaint", localmodels.ModelInput{Endpoint: modelmanager.BundledEndpoint, Name: "wf-a-inpaint", Model: edited}))
	wfrefresh.Refresh(ctx(), r.Models, r.Svc, cat, modelmanager.BundledEndpoint, warn)
	assert.InDelta(t, 0.25, names(t, r)["wf-a-inpaint"].CostPerImage, 1e-9, "the copy the user changed is kept")
	n := 0
	for _, w := range r.Svc.InstalledModels()["m-a"].Workflows {
		if w == "wf-a-inpaint" {
			n++
		}
	}
	assert.Equal(t, 1, n)

	require.NoError(t, r.Svc.DeleteModel(ctx(), "m-a"))
	assert.NotContains(t, names(t, r), "wf-a-inpaint", "the delete removes the workflow the start added")
	assert.NotContains(t, names(t, r), "wf-a")
	assert.Zero(t, warned)
}

// A workflow of a model that was not installed is not made, and the name of a copy the user deleted is not made again.
func TestNothingIsAddedForAModelThatIsNotInstalledOrForACopyTheUserDeleted(t *testing.T) {
	var cat *catalog.Catalog
	r := mmtest.New(t, func(d *modelmanager.Deps, _ *mmtest.Rig) { cat = d.Catalog })
	r.InstallEngine()
	require.Equal(t, domain.JobSucceeded, r.InstallModel("m-a").State)
	cat.Models[0].Workflows = append(cat.Models[0].Workflows, catalog.Workflow{Name: "wf-a-inpaint", Template: "inpaint-sdxl"})
	cat.Models[1].Workflows = append(cat.Models[1].Workflows, catalog.Workflow{Name: "wf-b-inpaint", Template: "inpaint-sdxl"})
	warn := func(string, ...any) {}

	wfrefresh.Refresh(ctx(), r.Models, r.Svc, cat, modelmanager.BundledEndpoint, warn)
	assert.Contains(t, names(t, r), "wf-a-inpaint")
	assert.NotContains(t, names(t, r), "wf-b-inpaint", "m-b is not installed")

	require.NoError(t, r.Models.DeleteModel(ctx(), modelmanager.BundledEndpoint, "wf-a-inpaint")) // the user deletes the copy
	wfrefresh.Refresh(ctx(), r.Models, r.Svc, cat, modelmanager.BundledEndpoint, warn)
	assert.NotContains(t, names(t, r), "wf-a-inpaint", "the name is in the record: it is not made again")
}
