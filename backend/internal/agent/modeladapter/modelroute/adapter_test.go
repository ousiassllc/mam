package modelroute_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// noop is a backend that answers nothing; only whether it exists matters to Adapter.Unavailable.
type noop struct{}

func (noop) GenerateImage(context.Context, string, modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	return modeladapter.ImageResult{}, nil
}

func (noop) GenerateVideo(context.Context, string, modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	return modeladapter.VideoResult{}, nil
}

// adapterOf is an Adapter with a backend for each of the models.
func adapterOf(t *testing.T, with ...domain.ModelName) *modeladapter.Adapter {
	t.Helper()
	publish(t)
	b := modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{}, Video: map[domain.ModelName]modeladapter.VideoBackend{}}
	for _, m := range with {
		b.Image[m], b.Video[m] = noop{}, noop{}
	}
	return modeladapter.New(localConfig(), b)
}

func TestForAdapter_WithoutTheApplicationsJudgementTheAdaptersOwnReasonIsUsed(t *testing.T) {
	a := adapterOf(t, chatgptid.Flare, localImage) // no backend for any API model

	env := modelroute.ForAdapter(a)
	pick, err := env.Image(modeladapter.PurposeNewReference)

	require.NoError(t, err)
	assert.Equal(t, localImage, pick.Route.Primary.Model)
	assert.Equal(t, domain.ModelGPTImage26Flare, pick.Passed[0].Model)
	assert.Contains(t, pick.Passed[0].Message, "API キー", "a model with no backend is a model with no key")
}

func TestForAdapter_TheJudgementOfTheApplicationDecidesWhatCanBeUsed(t *testing.T) {
	a := adapterOf(t, domain.ModelGPTImage26Flare, domain.ModelNanoBanana, chatgptid.Flare, localImage)
	a.Usable.SetAvailability(func(id string) (modelfilter.Availability, bool) {
		if id == string(domain.ModelGPTImage26Flare) {
			return modelfilter.Unusable(modelfilter.ReasonNoKey, "no key", ""), true
		}
		return modelfilter.Usable(), true
	})

	pick, err := modelroute.ForAdapter(a).Image(modeladapter.PurposeNewReference)

	require.NoError(t, err)
	assert.False(t, pick.Substituted(), "the alternative of the assignment can be used: the Adapter moves on to it by itself")
}

func TestForAdapter_TheAgeGateAndThePinAndTheAdultRatingComeFromTheAdapter(t *testing.T) {
	a := adapterOf(t, chatgptid.Flare, localImage, localText)
	a.Gate.Set(func(m domain.ModelName) bool { return m == domain.ModelNanoBanana })

	_, err := modelroute.ForAdapter(a).Image(modeladapter.PurposeNewReference)
	require.ErrorIs(t, err, agegate.ErrRefused, "an adult model of the assignment refuses the route whole")

	a.Gate.Set(nil)
	a.Usable.SetAdult(func(id string) bool { return id == string(localImage) })
	pick, err := modelroute.ForAdapter(a).Image(modeladapter.PurposeNewReference)
	require.NoError(t, err)
	assert.Equal(t, localText, pick.Route.Primary.Model, "the model rated adult is not chosen on its own")

	a.Usable.SetPinned(true)
	_, err = modelroute.ForAdapter(a).Image(modeladapter.PurposeNewReference)
	require.ErrorIs(t, err, modelroute.ErrNoUsableModel)
}

func TestEffective_IsTheAssignmentOfTheAdapterAsAStartWouldUseIt(t *testing.T) {
	a := adapterOf(t, chatgptid.Flare, localImage, localVideo)

	cfg := modelroute.Effective(a)()

	assert.Equal(t, localImage, cfg.ImageRouteFor(modeladapter.PurposeNewReference).Primary.Model)
	assert.Equal(t, localVideo, cfg.Policies[domain.QualityBalanced].Primary.Model, "the plan names and prices the model a start would use")
	assert.Equal(t, domain.ModelSeedance2, a.Config().Policies[domain.QualityBalanced].Primary.Model, "the running setting is left as it is")
}

func TestForAdapter_AProviderThatDoesNotServeAModelSaysSoAndTheMissingKeyIsTheProviders(t *testing.T) {
	publish(t)
	cfg := localConfig()
	cfg.VideoProvider = viaprovider.OpenRouter
	b := modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelKling3: noop{}, domain.ModelKling4: noop{}}}
	env := modelroute.ForAdapter(modeladapter.New(cfg, b))

	refused := env.Check(domain.ModelKling4)
	assert.Equal(t, modelfilter.ReasonProvider, refused.Reason)
	assert.Contains(t, refused.Message, "OpenRouter は Kling 4.0 を提供していません")
	assert.Equal(t, "/settings/api-keys", refused.Route)

	assert.True(t, env.Check(domain.ModelKling3).Available, "served, and the stub backend has no key to miss")
	noBackend := modelroute.ForAdapter(modeladapter.New(cfg, modeladapter.Backends{})).Check(domain.ModelKling3)
	assert.Equal(t, modelfilter.ReasonNoKey, noBackend.Reason)
	assert.Contains(t, noBackend.Message, "OpenRouter の API キー")
}
