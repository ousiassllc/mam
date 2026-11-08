package modelroute_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

func TestImage_AUsableAssignmentIsKept(t *testing.T) {
	publish(t)
	// The primary has no key, the alternative can be used: the Adapter moves on to it by itself, nothing is chosen in its place.
	env := modelroute.Env{Config: modeladapter.DefaultConfig(), Check: func(m domain.ModelName) modelfilter.Availability {
		if m == domain.ModelGPTImage26Flare {
			return modelfilter.Unusable(modelfilter.ReasonNoKey, "no key", "")
		}
		return modelfilter.Usable()
	}}

	pick, err := env.Image(modeladapter.PurposeNewReference)

	require.NoError(t, err)
	assert.False(t, pick.Substituted())
	assert.Equal(t, env.Config.Images[modeladapter.PurposeNewReference], pick.Route)
	assert.Equal(t, modelreg.SelectionAssignment, pick.Selection)
}

func TestImage_ADefaultAssignmentThatCannotBeUsedTakesTheFirstModelThatMatches(t *testing.T) {
	env := observed(t)

	fresh, err := env.Image(modeladapter.PurposeNewReference)
	require.NoError(t, err)
	edit, err := env.Image(modeladapter.PurposeEditReference)
	require.NoError(t, err)

	assert.True(t, fresh.Substituted())
	assert.Equal(t, localImage, fresh.Route.Primary.Model, "this PC first: free, and the plan's quota is not spent")
	assert.Equal(t, fresh.Route.Primary, fresh.Route.Fallback, "no alternative of another route and another cost")
	assert.Equal(t, modelreg.SelectionCondition, fresh.Selection)
	assert.Contains(t, fresh.Reason, "条件に合う利用可能なモデルを選びました")
	assert.NotContains(t, fresh.Reason, "最高", "a match of conditions, never a claim of quality")
	require.Len(t, fresh.Passed, 2)
	assert.Equal(t, domain.ModelGPTImage26Flare, fresh.Passed[0].Model)
	assert.Contains(t, fresh.Passed[0].Message, "API キー")
	assert.Equal(t, localImage, edit.Route.Primary.Model, "an edit needs a model that takes reference images: this one does")
}

func TestImage_ATextOnlyModelDoesNotServeAnEditOfReferences(t *testing.T) {
	env := observed(t)
	env.Check = noAPIKey(localImage) // what is left on this PC takes no reference image, or cannot run without one

	fresh, err := env.Image(modeladapter.PurposeNewReference)
	require.NoError(t, err)
	edit, err := env.Image(modeladapter.PurposeEditReference)
	require.NoError(t, err)

	assert.Equal(t, localText, fresh.Route.Primary.Model, "the workflow that requires a reference never makes a new picture, the text-only one does")
	assert.Equal(t, chatgptid.Flare, edit.Route.Primary.Model, "an edit of references is not made by a model that drops them, nor by one that cannot run without the picture it edits")
}

func TestImage_AnExplicitAssignmentIsNeverReplaced(t *testing.T) {
	cases := map[string]func(*modelroute.Env){
		"a route the user changed": func(e *modelroute.Env) {
			e.Config.Images = map[modeladapter.ImagePurpose]modeladapter.ImageRoute{}
			for k, v := range modeladapter.DefaultConfig().Images {
				e.Config.Images[k] = v
			}
			e.Config.Images[modeladapter.PurposeNewReference] = modeladapter.ImageRoute{
				Primary: modeladapter.ImageTarget{Model: domain.ModelFlux2}, Fallback: modeladapter.ImageTarget{Model: domain.ModelNanoBananaLite},
			}
		},
		"a file the operator pinned":     func(e *modelroute.Env) { e.Pinned = true },
		"the OpenAI route of the switch": func(e *modelroute.Env) { e.Config.PreferOpenAI = true },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			env := observed(t)
			change(&env)

			_, err := env.Image(modeladapter.PurposeNewReference)

			require.ErrorIs(t, err, modelroute.ErrNoUsableModel)
			var nu *modelroute.NoUsableError
			require.ErrorAs(t, err, &nu)
			assert.True(t, nu.Explicit)
			assert.Contains(t, models(nu.Candidates), localImage, "the usable models are offered as suggestions")
			assert.Contains(t, err.Error(), "自動で替えません")
			assert.Contains(t, err.Error(), string(localImage))
		})
	}
}

func TestImage_NothingUsableIsRefusedWithTheCauseAndTheModelsOfTheAssignment(t *testing.T) {
	publish(t)
	env := modelroute.Env{Config: modeladapter.DefaultConfig(), Check: noAPIKey()}
	env.Check = func(domain.ModelName) modelfilter.Availability {
		return modelfilter.Unusable(modelfilter.ReasonNoKey, "API キーが登録されていません", "/settings/api-keys")
	}

	_, err := env.Image(modeladapter.PurposeNewReference)

	require.ErrorIs(t, err, modelroute.ErrNoUsableModel)
	var nu *modelroute.NoUsableError
	require.ErrorAs(t, err, &nu)
	assert.False(t, nu.Explicit)
	assert.Empty(t, nu.Candidates)
	assert.Len(t, nu.Assigned, 2)
	assert.Equal(t, "/settings/api-keys", nu.Assigned[0].Route)
	assert.Contains(t, err.Error(), "gpt-image-2.6-flare＝API キーが登録されていません")
	assert.Contains(t, err.Error(), "nano-banana＝")
	assert.Contains(t, err.Error(), "条件に合って今使えるモデルもありません")
	assert.Equal(t, err.Error(), modelroute.CauseOf(errors.Join(errors.New("wrapped"), err)))
}

func TestImage_AnAdultModelIsNeverChosenOnItsOwn(t *testing.T) {
	env := observed(t)
	env.Adult = func(m domain.ModelName) bool { return m == localImage || m == localText }

	fresh, err := env.Image(modeladapter.PurposeNewReference)

	require.NoError(t, err)
	assert.Equal(t, chatgptid.Flare, fresh.Route.Primary.Model, "an adult model is skipped even with the age confirmation on")
}

func TestImage_TheAgeGateRefusesTheRouteWholeAndNothingReplacesIt(t *testing.T) {
	env := observed(t)
	var gate agegate.Gate
	gate.Set(func(m domain.ModelName) bool { return m == domain.ModelNanoBanana })
	env.Refuse = gate.Check

	_, err := env.Image(modeladapter.PurposeNewReference)

	require.ErrorIs(t, err, agegate.ErrRefused)
	assert.NotErrorIs(t, err, modelroute.ErrNoUsableModel)
}

func TestImage_TheLicenceOfAModelRulesItOutOfAChoiceMadeOnItsOwn(t *testing.T) {
	t.Cleanup(func() { modellicense.SetResolver(nil); modellicense.SetDefaultFilter(nil) })
	answers := map[string]modellicense.Commercial{string(localImage): modellicense.No, string(localText): modellicense.No}
	modellicense.SetResolver(func(id string) (modellicense.Info, bool) {
		if c, ok := answers[id]; ok {
			return modellicense.Info{Commercial: c, LicenseName: "x"}, true
		}
		return modellicense.Info{}, false
	})
	env := observed(t)

	no, err := env.Image(modeladapter.PurposeNewReference)
	require.NoError(t, err)
	assert.Equal(t, chatgptid.Flare, no.Route.Primary.Model, "a model whose licence says no commercial use is never chosen")

	answers[string(localImage)], answers[string(localText)] = modellicense.Unknown, modellicense.Unknown
	modellicense.SetDefaultFilter(func() modellicense.Filter { return modellicense.FilterOK })
	only, err := env.Image(modeladapter.PurposeNewReference)
	require.NoError(t, err)
	assert.Equal(t, chatgptid.Flare, only.Route.Primary.Model, "with 商用OKのモデルだけを使う, a model whose licence is not yes is left out")
	assert.Equal(t, modellicense.Yes, modellicense.Of(string(chatgptid.Flare)).Commercial)
}

func TestImage_TheOrderIsTheRouteThenTheKnownPriceThenTheIdentifier(t *testing.T) {
	env := observed(t)
	env.Pinned = true // the assignment cannot be replaced: the refusal lists the candidates in the order of the rule
	env.Check = func(m domain.ModelName) modelfilter.Availability {
		if m == domain.ModelGPTImage26Flare || m == domain.ModelNanoBanana {
			return modelfilter.Unusable(modelfilter.ReasonNoKey, "no key", "")
		}
		return modelfilter.Usable() // every other model: this PC, the subscription and the API models that have a key
	}

	candidates := func() []domain.ModelName {
		_, err := env.Image(modeladapter.PurposeNewReference)
		var nu *modelroute.NoUsableError
		require.ErrorAs(t, err, &nu)
		return models(nu.Candidates)
	}
	got := candidates()

	at := func(m domain.ModelName) int { return slices.Index(got, m) }
	assert.Equal(t, []domain.ModelName{localImage, localText}, got[:2], "this PC first, by identifier")
	assert.Less(t, at(localText), at(chatgptid.Flare))
	assert.Equal(t, []domain.ModelName{chatgptid.Flare, chatgptid.Sunburst, chatgptid.Model}, got[2:5], "then the subscription, by identifier")
	assert.Less(t, at(chatgptid.Model), at(domain.ModelFlux2))
	assert.Less(t, at(domain.ModelFlux2), at(domain.ModelNanoBananaLite), "an API: the cheaper known price first (0.03 before 0.0336)")
	assert.NotContains(t, got, localEdit, "it cannot make a picture without the one it edits")
	assert.Equal(t, got, candidates(), "the same answer every time")
}
