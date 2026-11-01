package app

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// adultOnTheBundledEngine is a model of the bundled endpoint that the real catalog's adult model chroma1-hd made: the model
// manager judges it an adult model by its workflow name, so the age gate (the confirmation was never given) must refuse it.
const adultOnTheBundledEngine = domain.ModelName("comfyui:bundled-comfyui/chroma1-hd")

func buildWithAdultModel(t *testing.T) (*modelsettings.Service, func() []domain.ModelName, func(domain.ModelName) bool) {
	t.Helper()
	cfg := testConfig(t)
	gpt := testChatGPT(cfg)
	logger := slog.New(slog.DiscardHandler)
	services, _, cleanup, err := buildServices(context.Background(), cfg, logger, gpt)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx := context.Background()
	require.NoError(t, services.LocalModels.CreateEndpoint(ctx, localmodels.EndpointInput{Name: modelmanager.BundledEndpoint, Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}))
	tpl, ok := localcfg.TemplateByID("txt2img-chroma")
	require.True(t, ok)
	require.NoError(t, services.LocalModels.CreateModel(ctx, localmodels.ModelInput{Endpoint: modelmanager.BundledEndpoint, Name: "chroma1-hd", Model: tpl.Model}))
	require.False(t, services.ModelManager.AdultContentEnabled(), "the age was never confirmed")

	settings := newSettings(cfg, services, gpt, newFFmpegDownloader(cfg, logger), logger)
	choices := func() []domain.ModelName {
		view, err := settings.Models.View()
		require.NoError(t, err)
		var out []domain.ModelName
		for _, o := range view.ImageModels {
			out = append(out, o.Model)
		}
		return out
	}
	return settings.Models, choices, services.Gentest.Adapter.Gate.Refuses
}

// The production wiring of the age gate: buildServices hands the model manager's predicate to the Model Adapter's Gate (every
// generation), newSettings to the assignment choices and Save. Dropping either line leaves the gate silently open.
func TestTheAgeGateIsWiredIntoTheAdapterAndTheSettings(t *testing.T) {
	sets, choices, refuses := buildWithAdultModel(t)

	assert.True(t, refuses(adultOnTheBundledEngine), "the Model Adapter refuses it (buildServices: Gate.Set)")
	assert.False(t, refuses(domain.ModelFlux2), "a built-in model is not refused")
	assert.NotContains(t, choices(), adultOnTheBundledEngine, "the choices leave it out (newSettings: SetHidden)")

	view, err := sets.View()
	require.NoError(t, err)
	in := modelsettings.Input{Images: view.Images, Policies: view.Policies}
	in.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{Primary: modeladapter.ImageTarget{Model: adultOnTheBundledEngine}, Fallback: modeladapter.ImageTarget{Model: domain.ModelFlux2}}
	var invalid *modelsettings.InvalidError
	require.ErrorAs(t, sets.Save(in), &invalid, "Save refuses the assignment (newSettings: SetHidden)")
	assert.Contains(t, invalid.Message, string(adultOnTheBundledEngine))
}
