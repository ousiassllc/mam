package mmview_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

func ids(es []mmview.Entry) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

// An installed model is available and an installed-only filter finds it; the others say they are not installed, and the route that installs them.
func TestEntries_AvailabilityFollowsTheInstallState(t *testing.T) {
	rig := mmtest.New(t)
	rig.Start()
	rig.InstallEngine()
	rig.InstallModel("m-b")

	by := map[string]mmview.Entry{}
	for _, e := range mmview.Entries(rig.Svc) {
		by[e.ID] = e
	}
	assert.True(t, by["m-b"].Available)
	assert.Empty(t, by["m-b"].Message)
	assert.False(t, by["m-a"].Available)
	assert.Equal(t, modelfilter.ReasonNotInstalled, by["m-a"].Reason)
	assert.Equal(t, "/model-manager/image-video", by["m-a"].Route)

	assert.Equal(t, []string{"m-b"}, ids(mmview.Where(rig.Svc, modelfilter.Condition{Installed: modelfilter.InstalledOnly})))
	assert.Equal(t, []string{"m-b"}, ids(mmview.Where(rig.Svc, modelfilter.Default())), "a picker's default shows what can be chosen")
	assert.Equal(t, []string{"m-a", "m-c"}, ids(mmview.Where(rig.Svc, modelfilter.Condition{Installed: modelfilter.NotInstalledOnly})))
}

func TestEntries_TraitsInputsAndTheFilterOfTheCatalog(t *testing.T) {
	rig := mmtest.New(t)
	rig.ConfirmAdult()
	got := map[string]mmview.Entry{}
	for _, e := range mmview.Entries(rig.Svc) {
		got[e.ID] = e
	}
	require.Contains(t, got, "m-d")
	assert.True(t, got["m-d"].Traits.Uncensored)
	assert.Contains(t, got["m-d"].Inputs, traits.T2I)
	assert.Equal(t, []string{"m-d"}, ids(mmview.Where(rig.Svc, modelfilter.Condition{Adult: modelfilter.AdultOnly, Uncensored: modelfilter.UncensoredOnly})))
	// 14B exactly is large, 5B exactly is small, 12B is medium
	assert.Equal(t, []string{"m-d"}, ids(mmview.Where(rig.Svc, modelfilter.Condition{Params: []modelfilter.Band{modelfilter.BandLarge}})))
	assert.Equal(t, []string{"m-b"}, ids(mmview.Where(rig.Svc, modelfilter.Condition{Params: []modelfilter.Band{modelfilter.BandSmall}, Kinds: []modelfilter.Kind{modelfilter.KindVideo}})))
	assert.Equal(t, []string{"m-c"}, ids(mmview.Where(rig.Svc, modelfilter.Condition{Params: []modelfilter.Band{modelfilter.BandMedium}})))
}

func TestInfoOf_FindsACatalogModelByTheNameOfItsWorkflow(t *testing.T) {
	rig := mmtest.New(t)
	info := mmview.InfoOf(rig.Svc)
	got, ok := info("comfyui:bundled-comfyui/wf-a-i2i")
	require.True(t, ok, "every workflow of a model names it")
	assert.InDelta(t, 2.6, got.Traits.ParamsB, 1e-9)
	assert.False(t, got.Adult)
	assert.Equal(t, 8, got.VRAMMinGB)
	d, ok := info("comfyui:bundled-comfyui/wf-d")
	require.True(t, ok)
	assert.True(t, d.Adult)
	assert.True(t, d.Traits.Uncensored)
	_, ok = info("comfyui:another/wf-a")
	assert.False(t, ok, "only the bundled endpoint's models are the catalog's")
	_, ok = info("flux-2")
	assert.False(t, ok, "a cloud model is not in the catalog")
	_, ok = info("comfyui:bundled-comfyui/nope")
	assert.False(t, ok)
}
