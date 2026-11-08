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

// #1113: the saved assignment is reported route by route by the rule a generation chooses its models with: a default route that cannot be used is shown with the
// model that would be used in its place (a match of conditions, not a ranking), a route the user set is shown as blocked with the candidates, and a pin blocks all.
func TestView_ReportsWhatEachRouteWouldRunOn(t *testing.T) {
	svc, _ := newService(t)
	svc.SetAvailability(func(id string) (modelfilter.Availability, bool) {
		if id == string(domain.ModelFlux2) || id == string(domain.ModelNanoBananaLite) {
			return modelfilter.Usable(), true // the preview purpose can be used; every other API model has no key
		}
		return modelfilter.Unusable(modelfilter.ReasonNoKey, "API キーが登録されていません", "/settings/api-keys"), true
	})
	v, err := svc.View()
	require.NoError(t, err)

	byKey := map[string]modelsettings.RouteReport{}
	for _, r := range v.Routes {
		byKey[r.Media+"/"+r.Key] = r
	}
	require.Len(t, v.Routes, len(modelsettings.ImagePurposes())+len(domain.QualityPolicies()))
	preview := byKey["image/preview"]
	assert.Equal(t, "assignment_usable", preview.Status)
	require.NotNil(t, preview.Use)
	assert.Equal(t, domain.ModelFlux2, preview.Use.Model)
	fresh := byKey["image/new_reference"]
	assert.Equal(t, "condition_match", fresh.Status, "the default assignment cannot be used: the usable model that matches is named")
	require.NotNil(t, fresh.Use)
	assert.Contains(t, []domain.ModelName{domain.ModelFlux2, domain.ModelNanoBananaLite}, fresh.Use.Model)
	assert.Contains(t, fresh.Reason, "条件に合う利用可能なモデルを選びました")
	assert.False(t, fresh.Explicit)
	video := byKey["video/balanced"]
	assert.Equal(t, "blocked", video.Status, "no video model can be used")
	assert.Nil(t, video.Use)
	assert.Contains(t, video.Message, "使えるモデルがありません")
	assert.Equal(t, "/settings/api-keys", fresh.Assigned[0].SettingsRoute)
}

func TestView_AnAssignmentTheUserSetIsReportedAsBlockedNotReplaced(t *testing.T) {
	svc, _ := newService(t)
	in := defaults()
	in.Images[modelsettings.PurposeNewReference] = modelsettings.ImageRoute{Primary: modelsettings.ImageTarget{Model: domain.ModelFlux2}, Fallback: modelsettings.ImageTarget{Model: domain.ModelFlux2}}
	require.NoError(t, svc.Save(in))
	svc.SetAvailability(func(id string) (modelfilter.Availability, bool) {
		if id == string(domain.ModelNanoBanana) {
			return modelfilter.Usable(), true
		}
		return modelfilter.Unusable(modelfilter.ReasonNoKey, "API キーが登録されていません", "/settings/api-keys"), true
	})
	v, err := svc.View()
	require.NoError(t, err)

	found := false
	for _, r := range v.Routes {
		if r.Media == "image" && r.Key == "new_reference" {
			found = true
			assert.True(t, r.Explicit, "the route was changed from the default")
			assert.Equal(t, "blocked", r.Status)
			assert.Nil(t, r.Use)
			require.NotEmpty(t, r.Candidates)
			assert.Equal(t, domain.ModelNanoBanana, r.Candidates[0].Model)
			assert.Contains(t, r.Message, "自動で替えません")
		}
	}
	require.True(t, found, "the report has the route of image/new_reference")
}

func TestView_AdultModelsAreNeverCandidatesOfTheReport(t *testing.T) {
	svc, _ := newService(t)
	svc.SetAvailability(func(id string) (modelfilter.Availability, bool) {
		if id == string(domain.ModelFlux2) {
			return modelfilter.Usable(), true
		}
		return modelfilter.Unusable(modelfilter.ReasonNoKey, "API キーが登録されていません", "/settings/api-keys"), true
	})
	svc.SetAdult(func(id string) bool { return id == string(domain.ModelFlux2) })
	v, err := svc.View()
	require.NoError(t, err)

	found := false
	for _, r := range v.Routes {
		if r.Media == "image" && r.Key == "new_reference" {
			found = true
			assert.Equal(t, "blocked", r.Status, "the only usable model is an adult one: it is not chosen on its own")
			assert.Empty(t, r.Candidates)
		}
	}
	require.True(t, found, "the report has the route of image/new_reference")
}
