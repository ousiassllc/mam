package modelroute_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

func TestVideo_ADefaultPolicyWhoseModelsCannotBeUsedTakesAModelThatFitsTheShot(t *testing.T) {
	env := observed(t)

	pick, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Keyframe: true})

	require.NoError(t, err)
	assert.True(t, pick.Substituted())
	want := modeladapter.VideoTarget{Model: localVideo, Resolution: "720p"}
	assert.Equal(t, want, pick.Route.Primary, "the Quality Policy's resolution, on the model of this PC that holds a 4 s Shot and takes the Keyframe")
	assert.Equal(t, want, pick.Route.Fallback)
	assert.Equal(t, env.Config.Policies[domain.QualityBalanced].Takes, pick.Route.Takes, "the number of Takes is the Policy's")
	assert.Equal(t, modelreg.SelectionCondition, pick.Selection)
	require.Len(t, pick.Passed, 2)
	assert.Equal(t, domain.ModelSeedance2, pick.Passed[0].Model)
}

func TestVideo_TheConditionsOfTheShotDecideWhichModelFits(t *testing.T) {
	env := observed(t)
	cases := []struct {
		name string
		need modelroute.VideoNeed
		want domain.ModelName
	}{
		{"a Keyframe: the first frame is used (and the text-to-video model cannot)", modelroute.VideoNeed{Seconds: 4, Keyframe: true}, localVideo},
		{"no Keyframe: an image-to-video workflow cannot run without a first frame", modelroute.VideoNeed{Seconds: 4, Keyframe: false}, localVideo2},
		{"a Shot only the longer clip holds", modelroute.VideoNeed{Seconds: 8, Keyframe: false}, localVideo2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pick, err := env.Video(domain.QualityBalanced, c.need)

			require.NoError(t, err)
			assert.Equal(t, c.want, pick.Route.Primary.Model)
		})
	}
}

func TestVideo_NoModelThatHoldsTheShotIsRefusedWithTheReasonItDoesNotMatch(t *testing.T) {
	env := observed(t)

	_, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 12, Keyframe: true})

	require.ErrorIs(t, err, modelroute.ErrNoUsableModel)
	var nu *modelroute.NoUsableError
	require.ErrorAs(t, err, &nu)
	assert.Empty(t, nu.Candidates)
	assert.Contains(t, err.Error(), "今使えるモデルはありますが、条件に合いません")
	assert.Contains(t, err.Error(), string(localVideo)+"＝尺 12 秒を 1 本に収められない")
}

func TestVideo_AResolutionTheModelDoesNotMakeIsNotAMatch(t *testing.T) {
	env := observed(t)
	env.Pinned = true // nothing is replaced: the refusal says which models were left out and why
	policies := map[domain.QualityPolicy]modeladapter.PolicyRoute{}
	for k, v := range env.Config.Policies {
		policies[k] = v
	}
	balanced := policies[domain.QualityBalanced]
	balanced.Primary.Resolution = "1080p"
	policies[domain.QualityBalanced] = balanced
	env.Config.Policies = policies

	_, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Keyframe: true})

	var nu *modelroute.NoUsableError
	require.ErrorAs(t, err, &nu)
	assert.Empty(t, nu.Candidates)
	assert.Contains(t, err.Error(), string(localVideo)+"＝解像度 1080p に対応しない")
}

func TestVideo_AFixedVideoModelThatCannotBeUsedIsNeverReplaced(t *testing.T) {
	env := observed(t)

	_, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Keyframe: true, Forced: domain.ModelKling3})

	var nu *modelroute.NoUsableError
	require.ErrorAs(t, err, &nu)
	assert.True(t, nu.Explicit)
	assert.Equal(t, domain.ModelKling3, nu.Assigned[0].Model)
	assert.Contains(t, models(nu.Candidates), localVideo, "the usable models are suggested")
}

func TestVideo_ACustomisedPolicyIsRefusedWithTheCandidatesAsSuggestions(t *testing.T) {
	env := observed(t)
	policies := map[domain.QualityPolicy]modeladapter.PolicyRoute{}
	for k, v := range env.Config.Policies {
		policies[k] = v
	}
	policies[domain.QualityBalanced] = modeladapter.PolicyRoute{
		Primary:  modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"},
		Fallback: modeladapter.VideoTarget{Model: domain.ModelRunwayGen45, Resolution: "720p"}, Takes: 2,
	}
	env.Config.Policies = policies

	_, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Keyframe: true})
	other, otherErr := env.Video(domain.QualityFast, modelroute.VideoNeed{Seconds: 4, Keyframe: true})

	var nu *modelroute.NoUsableError
	require.ErrorAs(t, err, &nu)
	assert.True(t, nu.Explicit)
	assert.Equal(t, []domain.ModelName{localVideo}, models(nu.Candidates))
	require.NoError(t, otherErr, "a Policy the user left alone is still the default's")
	assert.True(t, other.Substituted())
}

func TestVideo_AUsableModelOfThePolicyIsKeptAndTheAdapterMovesOnToIt(t *testing.T) {
	publish(t)
	env := modelroute.Env{Config: localConfig(), Check: noAPIKey()}
	env.Check = func(m domain.ModelName) modelfilter.Availability {
		if m == domain.ModelKling3 {
			return modelfilter.Usable()
		}
		return noAPIKey()(m)
	}

	pick, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Keyframe: true})

	require.NoError(t, err)
	assert.False(t, pick.Substituted())
	assert.Equal(t, env.Config.Policies[domain.QualityBalanced], pick.Route)
}
