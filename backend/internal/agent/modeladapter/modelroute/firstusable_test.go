package modelroute_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

func TestFirstUsable_IsThePrimaryUnlessOnlyTheAlternativeCanBeUsed(t *testing.T) {
	publish(t)
	cases := []struct {
		name   string
		usable []domain.ModelName
		want   domain.ModelName
	}{
		{"both", []domain.ModelName{domain.ModelSeedance2, domain.ModelKling3}, domain.ModelSeedance2},
		{"only the primary", []domain.ModelName{domain.ModelSeedance2}, domain.ModelSeedance2},
		{"only the alternative: the Adapter skips the primary for it", []domain.ModelName{domain.ModelKling3}, domain.ModelKling3},
		{"neither: the primary, which the start refuses", nil, domain.ModelSeedance2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := modelroute.Env{Config: localConfig(), Check: func(m domain.ModelName) modelfilter.Availability {
				for _, u := range c.usable {
					if u == m {
						return modelfilter.Usable()
					}
				}
				return noAPIKey()(m)
			}}

			assert.Equal(t, c.want, env.FirstUsable(domain.ModelSeedance2, domain.ModelKling3))
		})
	}
}

func TestNamed_NamesTheAlternativeThatTheAdapterRunsWhenThePrimaryCannotBeUsed(t *testing.T) {
	publish(t)
	env := modelroute.Env{Config: localConfig(), Check: func(m domain.ModelName) modelfilter.Availability {
		if m == domain.ModelKling3 {
			return modelfilter.Usable()
		}
		return noAPIKey()(m)
	}}
	pick, err := env.Video(domain.QualityBalanced, modelroute.VideoNeed{Seconds: 4, Keyframe: true})
	require.NoError(t, err)
	require.False(t, pick.Substituted())

	card := env.Named(pick.Selected, pick.Route.Primary.Model, pick.Route.Fallback.Model, "video")

	assert.Equal(t, string(domain.ModelKling3)+"（API・従量課金）", card)
}

// The availability check reads credentials and the disk: one Report or Effective asks it once per model, however many routes name the model.
func TestReportAndEffective_AskTheAvailabilityOncePerModel(t *testing.T) {
	publish(t)
	calls := map[domain.ModelName]int{}
	env := modelroute.Env{Config: localConfig(), Check: func(m domain.ModelName) modelfilter.Availability {
		calls[m]++
		return noAPIKey()(m)
	}}

	first := env.Report()
	for m, n := range calls {
		assert.Equal(t, 1, n, "Report asked about %s %d times", m, n)
	}
	clear(calls)
	env.Effective()
	for m, n := range calls {
		assert.Equal(t, 1, n, "Effective asked about %s %d times", m, n)
	}

	assert.NotEmpty(t, calls)
	assert.Equal(t, first, env.Report(), "the same answer every time: the cache lives for one call only")
	assert.Len(t, first, len(modeladapter.ImagePurposes())+len(domain.QualityPolicies()))
}
