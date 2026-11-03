package modelsettings_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// noKey makes the given models unusable: the judgement the app takes from the generation test (gentest.Service.Availability).
func noKey(models ...domain.ModelName) func(string) (modelfilter.Availability, bool) {
	return func(id string) (modelfilter.Availability, bool) {
		for _, m := range models {
			if string(m) == id {
				return modelfilter.Unusable(modelfilter.ReasonNoKey, "API キーが登録されていません", "/settings/api-keys"), true
			}
		}
		return modelfilter.Usable(), true
	}
}

// #1013: the choices say whether each model can be used now; the routes that still name one are listed so that the screen warns about them.
func TestView_SaysWhichChoicesAndRoutesCannotBeUsed(t *testing.T) {
	svc, _ := newService(t)
	v, err := svc.View()
	require.NoError(t, err)
	for _, o := range v.ImageModels {
		assert.True(t, o.Availability.Available, string(o.Model)+": nothing is judged until the app registers the judgement")
	}
	assert.Empty(t, v.Unusable)

	primary := v.Images[modelsettings.ImagePurposes()[0]].Primary.Model
	svc.SetAvailability(noKey(primary, domain.ModelSeedance2))
	v, err = svc.View()
	require.NoError(t, err)
	var seen bool
	for _, o := range v.ImageModels {
		if o.Model == primary {
			seen = true
			assert.False(t, o.Availability.Available, "the choice stays (the assignment screen lists every model) and says it cannot be used")
			assert.Equal(t, "/settings/api-keys", o.Availability.Route)
		} else {
			assert.True(t, o.Availability.Available, string(o.Model))
		}
	}
	assert.True(t, seen)
	var videoSeen bool
	for _, o := range v.VideoModels {
		if o.Model == domain.ModelSeedance2 {
			videoSeen = true
			assert.False(t, o.Availability.Available)
		}
	}
	assert.True(t, videoSeen)
	require.NotEmpty(t, v.Unusable)
	for _, u := range v.Unusable {
		assert.Contains(t, []string{string(primary), string(domain.ModelSeedance2)}, string(u.Model))
		assert.Contains(t, []string{"image", "video"}, u.Media)
		assert.Contains(t, []string{"primary", "fallback"}, u.Role)
		assert.NotEmpty(t, u.Availability.Message)
	}
}

func TestView_ChoicesCanBeNarrowedByTheFilter(t *testing.T) {
	svc, _ := newService(t)
	svc.SetAvailability(noKey(domain.ModelSeedance2))
	v, err := svc.View()
	require.NoError(t, err)

	all := v.VideoModelsWhere(modelfilter.Condition{})
	assert.Len(t, all, len(v.VideoModels), "the zero condition shows everything")
	usable := v.VideoModelsWhere(modelfilter.Default())
	assert.Len(t, usable, len(v.VideoModels)-1)
	for _, o := range usable {
		assert.NotEqual(t, domain.ModelSeedance2, o.Model)
	}
	assert.Len(t, v.VideoModelsWhere(modelfilter.Condition{Text: "seedance-2.0"}), 1, "the search reads the model name")
	assert.NotEmpty(t, v.ImageModelsWhere(modelfilter.Condition{Origins: []modelfilter.Origin{modelfilter.OriginAPI}}))
	assert.Empty(t, v.ImageModelsWhere(modelfilter.Condition{Origins: []modelfilter.Origin{modelfilter.OriginLocal}}), "no local model is registered in this setting")
}
