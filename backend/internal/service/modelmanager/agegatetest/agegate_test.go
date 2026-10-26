// Package agegatetest runs the age gate end to end over an installed adult model of the model manager's rig
// (spec/architecture/model-manager/model-manager.md §6.1): what the assignment choices, the Model Adapter and the delete do when the age
// confirmation is turned off while an adult model is installed and assigned.
package agegatetest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

const adult = domain.ModelName("comfyui:bundled-comfyui/wf-d")

var ctx = context.Background

// adultRig has the engine and the adult model m-d installed with the age gate open, as a user who confirmed would.
func adultRig(t *testing.T) *mmtest.Rig {
	t.Helper()
	r := mmtest.New(t)
	r.InstallEngine()
	r.ConfirmAdult()
	require.Equal(t, domain.JobSucceeded, r.InstallModel("m-d").State)
	return r
}

func localModels(t *testing.T, r *mmtest.Rig) []string {
	t.Helper()
	view, err := r.Models.View()
	require.NoError(t, err)
	var out []string
	for _, m := range view.Models {
		out = append(out, m.ID)
	}
	return out
}

func imageChoices(t *testing.T, r *mmtest.Rig) []domain.ModelName {
	t.Helper()
	view, err := r.Sets.View()
	require.NoError(t, err)
	var out []domain.ModelName
	for _, o := range view.ImageModels {
		out = append(out, o.Model)
	}
	return out
}

// An installed adult model that the preview purpose names (primary; the alternative is another model): with the age
// confirmation off it leaves the choices, the generation of the purpose is refused, and so is a direct call; no call reaches the
// bundled ComfyUI (it is not even started), and the alternative does not take over. Turned on again, the same assignment is called.
func TestAnAssignedAdultModelIsOutOfTheChoicesAndRefusedWhileTheGateIsOff(t *testing.T) {
	r := adultRig(t)
	assert.Contains(t, imageChoices(t, r), adult)
	view, err := r.Sets.View()
	require.NoError(t, err)
	in := modelsettings.Input{Images: view.Images, Policies: view.Policies}
	in.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{Primary: modeladapter.ImageTarget{Model: adult}, Fallback: modeladapter.ImageTarget{Model: domain.ModelNanoBanana}}
	require.NoError(t, r.Sets.Save(in), "chosen while the gate is open")
	require.Equal(t, proc.StateStopped, r.Svc.Engine().Process.State)

	require.NoError(t, r.Svc.SetAdultContent(false, nil))

	assert.NotContains(t, imageChoices(t, r), adult)
	view, err = r.Sets.View()
	require.NoError(t, err)
	assert.Equal(t, []domain.ModelName{adult}, view.Gated)
	assert.Equal(t, adult, view.Images[modeladapter.PurposePreview].Primary.Model, "the assignment itself is kept")
	_, err = r.Adapter.GenerateImage(ctx(), modeladapter.PurposePreview, modeladapter.ImageRequest{Prompt: "p"})
	require.ErrorIs(t, err, agegate.ErrRefused, "refused, not routed to the alternative and not a failure of a backend")
	_, err = direct.Image(ctx(), r.Adapter, adult, modeladapter.ImageRequest{Prompt: "p"})
	require.ErrorIs(t, err, agegate.ErrRefused)
	assert.Equal(t, proc.StateStopped, r.Svc.Engine().Process.State, "no call reached ComfyUI: the bundled engine was not started for it")

	require.NoError(t, r.Svc.SetAdultContent(true, nil))
	_, err = direct.Image(ctx(), r.Adapter, adult, modeladapter.ImageRequest{Prompt: "p"})
	require.Error(t, err, "the fake engine answers /system_stats only")
	assert.NotErrorIs(t, err, agegate.ErrRefused)
	assert.Equal(t, proc.StateRunning, r.Svc.Engine().Process.State, "with the gate open the call starts the bundled engine")
	assert.Contains(t, imageChoices(t, r), adult)
}

// Deleting stays possible while the age confirmation is off: the model and its files go.
func TestAnAdultModelCanBeDeletedWhileTheGateIsOff(t *testing.T) {
	r := adultRig(t)
	require.FileExists(t, r.ModelFile(mmtest.LoraD))
	require.Contains(t, localModels(t, r), string(adult))
	require.NoError(t, r.Svc.SetAdultContent(false, nil))

	require.NoError(t, r.Svc.DeleteModel(ctx(), "m-d"))

	assert.NoFileExists(t, r.ModelFile(mmtest.LoraD))
	assert.NotContains(t, localModels(t, r), string(adult), "the model of the workflow is removed")
	_, err := r.Svc.Model("m-d")
	assert.ErrorIs(t, err, modelmanager.ErrAdultNotConfirmed, "and the model manager still hides it")
}
