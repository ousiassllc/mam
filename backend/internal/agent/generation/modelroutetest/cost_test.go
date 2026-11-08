package modelroutetest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute/routetest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// A budget ceiling keeps applying to the model that was chosen in place of the assignment (spec/architecture/model-adapters.md §3.1): a model on this PC is free
// and the subscription has no unit price, so neither can pass a ceiling; a priced API model is held to it like any other.
func TestCost_ACeilingStillAppliesToAPricedModelChosenInPlaceOfTheAssignment(t *testing.T) {
	r := newRig(t, nil)
	r.usable = only(domain.ModelFlux2, domain.ModelNanoBananaLite) // the only models that can be used are API models with a price
	require.NoError(t, r.policies.SetBudget(context.Background(), r.projectID, new(0.0001), nil))

	_, err := r.orch.StartReferencePack(context.Background(), r.shotID)

	assert.ErrorIs(t, err, generation.ErrBudgetExceeded)
	assert.Zero(t, r.jobs.count())
}

func TestCost_AFreeOrUnpricedRouteIsNotCountedAgainstACeiling(t *testing.T) {
	cases := map[string]func(domain.ModelName) modelfilter.Availability{
		"this PC (free)":                   routetest.NoAPIKey(),
		"the subscription (no unit price)": only(chatgptid.Flare, chatgptid.Sunburst, chatgptid.Model),
	}
	for name, usable := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, nil)
			r.usable = usable
			require.NoError(t, r.policies.SetBudget(context.Background(), r.projectID, new(0.0001), nil))

			require.NoError(t, r.runPack(t), "an unknown price is not counted: the existing rule of the models without a price")

			for _, ref := range r.shot(t).Shot.References() {
				require.NotNil(t, ref.Use)
				assert.NotEqual(t, modelreg.RouteAPI, ref.Use.Route)
			}
			assert.Zero(t, r.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE estimated_cost > 0`))
		})
	}
}

func TestCost_TheVideoOfAModelOnThisPCIsFreeAndKeepsEveryTake(t *testing.T) {
	r := newRig(t, nil)
	require.NoError(t, r.runPack(t))
	require.NoError(t, r.policies.SetBudget(context.Background(), r.projectID, new(0.0001), nil))

	require.NoError(t, r.runGenerate(t))

	assert.Equal(t, 3, r.count(t, `SELECT COUNT(*) FROM take`), "a free model is not shrunk to fewer Takes by a ceiling")
}
