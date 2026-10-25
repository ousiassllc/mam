package modelsettings_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

const (
	adultImage = domain.ModelName("comfyui:bundled-comfyui/adult-img")
	adultVideo = domain.ModelName("comfyui:bundled-comfyui/adult-vid")
)

// gatedService is a service over a file that defines an image and a video model on the bundled endpoint, whose gate refuses
// them while closed is true.
func gatedService(t *testing.T) (svc *modelsettings.Service, closed *bool, path string) {
	t.Helper()
	models := map[string]localcfg.Model{}
	for name, tpl := range map[string]string{"adult-img": "txt2img-sdxl", "adult-vid": "txt2video-wan22-5b"} {
		tm, ok := localcfg.TemplateByID(tpl)
		require.True(t, ok)
		models["bundled-comfyui/"+name] = tm.Model
	}
	file, err := json.Marshal(map[string]any{
		"endpoints":    map[string]localcfg.Endpoint{"bundled-comfyui": {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"}},
		"local_models": models,
	})
	require.NoError(t, err)
	path = filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(path, file, 0o600))
	cfg, err := configcheck.Load(path)
	require.NoError(t, err)
	localbackends.Publish(cfg)
	t.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })

	svc, closed = modelsettings.New(path, false), new(bool)
	svc.SetHidden(func(id string) bool { return *closed && (id == string(adultImage) || id == string(adultVideo)) })
	return svc, closed, path
}

// assigned is the default assignment with the adult models as the primary of the preview purpose and of the Fast policy.
func assigned(t *testing.T, svc *modelsettings.Service) modelsettings.Input {
	t.Helper()
	view, err := svc.View()
	require.NoError(t, err)
	in := modelsettings.Input{Images: view.Images, Policies: view.Policies}
	in.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{Primary: modeladapter.ImageTarget{Model: adultImage}, Fallback: modeladapter.ImageTarget{Model: domain.ModelFlux2}}
	fast := in.Policies[domain.QualityFast]
	fast.Primary = modeladapter.VideoTarget{Model: adultVideo, Resolution: "720p"}
	in.Policies[domain.QualityFast] = fast
	return in
}

func imageNames(v modelsettings.View) []domain.ModelName {
	var out []domain.ModelName
	for _, o := range v.ImageModels {
		out = append(out, o.Model)
	}
	return out
}

func videoNames(v modelsettings.View) []domain.ModelName {
	var out []domain.ModelName
	for _, o := range v.VideoModels {
		out = append(out, o.Model)
	}
	return out
}

// With the age confirmation off an adult model is not among the choices, and a new assignment of it is refused with a reason
// that names the way on; with it on the model is a choice like any other.
func TestAdultModelsAreChoicesOnlyWhileTheGateIsOpen(t *testing.T) {
	svc, closed, path := gatedService(t)
	view, err := svc.View()
	require.NoError(t, err)
	assert.Contains(t, imageNames(view), adultImage)
	assert.Contains(t, videoNames(view), adultVideo)
	assert.Empty(t, view.Gated)

	*closed = true
	view, err = svc.View()
	require.NoError(t, err)
	assert.NotContains(t, imageNames(view), adultImage)
	assert.NotContains(t, videoNames(view), adultVideo)
	assert.Contains(t, imageNames(view), domain.ModelNanoBanana, "the other models are still offered")
	assert.Contains(t, videoNames(view), domain.ModelKling3)

	var invalid *modelsettings.InvalidError
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	require.ErrorAs(t, svc.Save(assigned(t, svc)), &invalid)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a refused save writes nothing")
	assert.Contains(t, invalid.Message, string(adultImage))
	assert.Contains(t, invalid.Message, "年齢確認がオフ")

	*closed = false
	require.NoError(t, svc.Save(assigned(t, svc)))
}

// An assignment that was saved while the gate was open stays in the file and is shown as one that cannot be used: the view
// lists the adult models it names (once), Save refuses it as long as it names one, changing the slots or resetting clears it,
// and opening the gate again makes the same assignment usable without a new save.
func TestAnAssignmentSavedBeforeTheGateClosedIsReportedAndCanOnlyBeReplaced(t *testing.T) {
	svc, closed, _ := gatedService(t)
	require.NoError(t, svc.Save(assigned(t, svc)))
	*closed = true

	view, err := svc.View()
	require.NoError(t, err)
	assert.Equal(t, []domain.ModelName{adultImage, adultVideo}, view.Gated)
	assert.Equal(t, adultImage, view.Images[modeladapter.PurposePreview].Primary.Model, "the saved assignment is shown as it is")
	assert.True(t, view.Customized)

	var invalid *modelsettings.InvalidError
	in := modelsettings.Input{Images: view.Images, Policies: view.Policies}
	require.ErrorAs(t, svc.Save(in), &invalid, "even saved unchanged: the form must not keep a route that cannot be used")
	assert.Contains(t, invalid.Message, "年齢確認をオンにするか")

	in.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{Primary: modeladapter.ImageTarget{Model: domain.ModelNanoBanana}, Fallback: modeladapter.ImageTarget{Model: domain.ModelFlux2}}
	require.ErrorAs(t, svc.Save(in), &invalid, "the video route still names an adult model")
	assert.Contains(t, invalid.Message, string(adultVideo))
	in.Policies[domain.QualityFast] = modeladapter.PolicyRoute{
		Primary: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}, Fallback: modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "720p"}, Takes: 2,
	}
	require.NoError(t, svc.Save(in))
	view, err = svc.View()
	require.NoError(t, err)
	assert.Empty(t, view.Gated)

	*closed = false
	require.NoError(t, svc.Save(assigned(t, svc)), "saving is refused only while the gate is closed")
}

// The alternative of a route is a model the route calls when the primary fails: an adult model there is reported and refused as
// much as one in the primary slot, for an image purpose and for a Quality Policy alike.
func TestAnAdultModelOnlyInTheAlternativeSlotIsReportedAndRefused(t *testing.T) {
	svc, closed, _ := gatedService(t)
	view, err := svc.View()
	require.NoError(t, err)
	in := modelsettings.Input{Images: view.Images, Policies: view.Policies}
	in.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{Primary: modeladapter.ImageTarget{Model: domain.ModelFlux2}, Fallback: modeladapter.ImageTarget{Model: adultImage}}
	in.Policies[domain.QualityFast] = modeladapter.PolicyRoute{
		Primary: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}, Fallback: modeladapter.VideoTarget{Model: adultVideo, Resolution: "720p"}, Takes: 2,
	}
	require.NoError(t, svc.Save(in))
	*closed = true

	view, err = svc.View()
	require.NoError(t, err)
	assert.Equal(t, []domain.ModelName{adultImage, adultVideo}, view.Gated)
	var invalid *modelsettings.InvalidError
	require.ErrorAs(t, svc.Save(modelsettings.Input{Images: view.Images, Policies: view.Policies}), &invalid)
}

func TestResettingTheAssignmentWorksWhileTheGateIsClosed(t *testing.T) {
	svc, closed, _ := gatedService(t)
	require.NoError(t, svc.Save(assigned(t, svc)))
	*closed = true

	require.NoError(t, svc.Reset())

	view, err := svc.View()
	require.NoError(t, err)
	assert.Empty(t, view.Gated)
	assert.False(t, view.Customized)
}
