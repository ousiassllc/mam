package routingtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// record is a Judge record of n Takes that all scored the same on every axis.
func record(m domain.ModelName, n int, score, passRate float64) domain.ModelRecord {
	return domain.ModelRecord{
		Model: m, Samples: n, PassRate: passRate,
		Scores: domain.TakeScores{Product: score, Acting: score, Camera: score, Continuity: score},
	}
}

func recordsOf(rs ...domain.ModelRecord) map[domain.ModelName]domain.ModelRecord {
	out := map[domain.ModelName]domain.ModelRecord{}
	for _, r := range rs {
		out[r.Model] = r
	}
	return out
}

func route(t *testing.T, policy domain.QualityPolicy, in generation.RoutingInput) generation.Strategy {
	t.Helper()
	s, err := generation.DecideRouted(modeladapter.DefaultConfig(), policy, 4, repository.Budget{}, "", &in)
	require.NoError(t, err)
	return s
}

func candidate(t *testing.T, d domain.RoutingDecision, m domain.ModelName) domain.RoutingCandidate {
	t.Helper()
	for _, c := range d.Candidates {
		if c.Model == m {
			return c
		}
	}
	require.Failf(t, "candidate missing", "%s is not among the candidates of %+v", m, d)
	return domain.RoutingCandidate{}
}

func TestRoute_WithoutJudgeRecordTheQualityPolicyStays(t *testing.T) {
	for _, p := range domain.QualityPolicies() {
		want, err := generation.Decide(modeladapter.DefaultConfig(), p, 4, repository.Budget{})
		require.NoError(t, err)

		got := route(t, p, generation.RoutingInput{MinAcceptance: 60})

		assert.Equal(t, want.Primary, got.Primary, p)
		assert.Equal(t, want.Fallback, got.Fallback, p)
		assert.Equal(t, want.Takes, got.Takes, p)
		assert.Equal(t, domain.RoutingPolicyDefault, got.Routing.Mode, p)
		assert.Equal(t, want.Primary.Model, got.Routing.Primary.Model, p)
		assert.Equal(t, want.Primary.Model, got.Routing.PolicyPrimary.Model, p)
	}
}

func TestRoute_ATrackedChallengerReplacesThePrimaryAndTheOldPrimaryBecomesTheAlternative(t *testing.T) {
	// BALANCED starts with Seedance 2.0. Kling 3.0 has a clearly better record at a similar price.
	s := route(t, domain.QualityBalanced, generation.RoutingInput{
		MinAcceptance: 75,
		Records:       recordsOf(record(domain.ModelKling3, 6, 95, 1), record(domain.ModelSeedance2, 6, 70, 0.4)),
	})

	assert.Equal(t, domain.ModelKling3, s.Primary.Model)
	assert.Equal(t, "720p", s.Primary.Resolution)
	assert.Equal(t, domain.ModelSeedance2, s.Fallback.Model)
	assert.Equal(t, domain.RoutingAdaptive, s.Routing.Mode)
	assert.Equal(t, domain.ModelSeedance2, s.Routing.PolicyPrimary.Model, "the Quality Policy's assignment stays on the record")
	assert.Equal(t, domain.ModelKling3, s.Routing.Primary.Model)
	assert.Contains(t, s.Routing.Reason, "kling-3.0")
	assert.Contains(t, s.Reasons[len(s.Reasons)-1], "multi-model routing")
	assert.Empty(t, candidate(t, s.Routing, domain.ModelKling3).Excluded)
}

func TestRoute_ChallengersThatDoNotQualifyAreExcludedWithAReason(t *testing.T) {
	cases := []struct {
		name    string
		records map[domain.ModelName]domain.ModelRecord
		min     int
		want    string
	}{
		{"too few judged Takes", recordsOf(record(domain.ModelKling3, 4, 99, 1)), 75, domain.ExcludedInsufficientEvidence},
		{"average under the Minimum acceptance score", recordsOf(record(domain.ModelKling3, 9, 70, 1), record(domain.ModelSeedance2, 9, 20, 0)), 75, domain.ExcludedBelowMinimumScore},
		{"not better by the switch margin", recordsOf(record(domain.ModelKling3, 9, 80, 0.8), record(domain.ModelSeedance2, 9, 80, 0.8)), 75, domain.ExcludedMargin},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := route(t, domain.QualityBalanced, generation.RoutingInput{MinAcceptance: c.min, Records: c.records})

			assert.Equal(t, domain.ModelSeedance2, s.Primary.Model, "the Quality Policy's primary stays")
			assert.Equal(t, domain.RoutingPolicyDefault, s.Routing.Mode)
			assert.Equal(t, c.want, candidate(t, s.Routing, domain.ModelKling3).Excluded)
		})
	}
}

func TestRoute_AVendorWhoseBreakerIsOpenIsNeverChosen(t *testing.T) {
	unavailable := func(m domain.ModelName) string {
		if m == domain.ModelKling3 {
			return domain.ExcludedCircuitOpen
		}
		return ""
	}
	s := route(t, domain.QualityBalanced, generation.RoutingInput{
		MinAcceptance: 75, Unavailable: unavailable,
		Records: recordsOf(record(domain.ModelKling3, 9, 99, 1), record(domain.ModelSeedance2, 9, 60, 0.2)),
	})

	assert.Equal(t, domain.ModelSeedance2, s.Primary.Model)
	assert.Equal(t, domain.ExcludedCircuitOpen, candidate(t, s.Routing, domain.ModelKling3).Excluded)
}

func TestRoute_TheBestOfSeveralQualifyingChallengersWins(t *testing.T) {
	// MAX QUALITY starts with Runway. Kling 1080p and Seedance 1080p both beat it; Seedance is better.
	s := route(t, domain.QualityMaxQuality, generation.RoutingInput{
		MinAcceptance: 85,
		Records: recordsOf(
			record(domain.ModelRunwayGen45, 8, 70, 0.3), record(domain.ModelKling3, 8, 90, 0.9), record(domain.ModelSeedance2, 8, 99, 1)),
	})

	assert.Equal(t, domain.ModelSeedance2, s.Primary.Model)
	assert.Equal(t, "1080p", s.Primary.Resolution)
	assert.Equal(t, domain.ModelRunwayGen45, s.Fallback.Model)
	assert.Equal(t, domain.ExcludedOutscored, candidate(t, s.Routing, domain.ModelKling3).Excluded)
}

func TestRoute_ShotNatureDecidesWhichScoreAxesCount(t *testing.T) {
	// Kling is excellent at camera work and weak on product accuracy; Seedance is the other way round.
	kling := domain.ModelRecord{Model: domain.ModelKling3, Samples: 9, PassRate: 0.8, Scores: domain.TakeScores{Product: 40, Acting: 80, Camera: 100, Continuity: 80}}
	seedance := domain.ModelRecord{Model: domain.ModelSeedance2, Samples: 9, PassRate: 0.8, Scores: domain.TakeScores{Product: 100, Acting: 80, Camera: 40, Continuity: 80}}
	records := recordsOf(kling, seedance, record(domain.ModelRunwayGen45, 9, 60, 0.5)) // MAX QUALITY's own primary is mediocre

	camera := &domain.ShotSpec{Camera: domain.SpecCamera{Framing: "wide", Movement: "fast dolly in"}}
	product := &domain.ShotSpec{Camera: domain.SpecCamera{Framing: "close-up", Movement: "static"}, Product: &domain.SpecProduct{ID: "p"}}

	onCamera := route(t, domain.QualityMaxQuality, generation.RoutingInput{MinAcceptance: 60, Records: records, Spec: camera})
	onProduct := route(t, domain.QualityMaxQuality, generation.RoutingInput{MinAcceptance: 60, Records: records, Spec: product})

	require.NotNil(t, onCamera.Routing.AxisWeights)
	assert.Greater(t, onCamera.Routing.AxisWeights.Camera, onCamera.Routing.AxisWeights.Product)
	assert.Greater(t, onProduct.Routing.AxisWeights.Product, onProduct.Routing.AxisWeights.Camera)
	assert.Equal(t, domain.ModelKling3, onCamera.Primary.Model, "a camera-driven Shot goes to the model that is good at camera work")
	assert.Equal(t, domain.ModelSeedance2, onProduct.Primary.Model, "a product Shot goes to the model that is good at products")
}

func TestRoute_CostCountsMoreUnderFastThanUnderMaxQuality(t *testing.T) {
	// The record is the same for both models: only the price differs (Seedance 480p is the cheapest).
	records := recordsOf(record(domain.ModelSeedance2, 9, 82, 0.8), record(domain.ModelKling3, 9, 82, 0.8), record(domain.ModelRunwayGen45, 9, 82, 0.8))

	fast := route(t, domain.QualityFast, generation.RoutingInput{MinAcceptance: 60, Records: records})
	max := route(t, domain.QualityMaxQuality, generation.RoutingInput{MinAcceptance: 60, Records: records})

	assert.Equal(t, domain.ModelSeedance2, fast.Primary.Model, "FAST takes the cheapest model when the records are equal")
	assert.Equal(t, "480p", fast.Primary.Resolution)
	assert.Equal(t, domain.ModelRunwayGen45, max.Primary.Model, "MAX QUALITY keeps its primary when the records are equal")
}

func TestRoute_CapabilityFitCountsAgainstAModelThatCannotTakeTheReferences(t *testing.T) {
	refs := []domain.Reference{{Kind: domain.RefKeyframe}, {Kind: domain.RefCharacter}}
	s := route(t, domain.QualityBalanced, generation.RoutingInput{MinAcceptance: 75, References: refs})

	assert.InDelta(t, 1.0, candidate(t, s.Routing, domain.ModelSeedance2).Fit, 1e-9, "Seedance takes a first frame and free references")
	assert.InDelta(t, 2.0/3, candidate(t, s.Routing, domain.ModelKling3).Fit, 1e-9, "Kling has no free reference images")
}

func TestRoute_FixedModelAndDisabledRoutingAreRecordedAndNotRouted(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	in := generation.RoutingInput{MinAcceptance: 75, Records: recordsOf(record(domain.ModelKling3, 9, 99, 1), record(domain.ModelSeedance2, 9, 60, 0.2))}

	fixed, err := generation.DecideRouted(cfg, domain.QualityBalanced, 4, repository.Budget{}, domain.ModelRunwayGen45, &in)
	require.NoError(t, err)
	cfg.Routing.Enabled = false
	off, err := generation.DecideRouted(cfg, domain.QualityBalanced, 4, repository.Budget{}, "", &in)
	require.NoError(t, err)

	assert.Equal(t, domain.RoutingFixed, fixed.Routing.Mode)
	assert.Equal(t, domain.ModelRunwayGen45, fixed.Primary.Model)
	assert.Equal(t, domain.RoutingDisabled, off.Routing.Mode)
	assert.Equal(t, domain.ModelSeedance2, off.Primary.Model)
	assert.Empty(t, off.Routing.Candidates)
}

func TestRoute_TheBudgetIsAppliedToTheRoutedModels(t *testing.T) {
	// Kling wins the routing. A 5 s Take costs 0.35 USD on Kling and 0.40 USD on Seedance (the Quality
	// Policy's primary), so a 0.75 USD ceiling allows 2 Takes with the routed model but only 1 with the default.
	ceiling := 0.75
	budget := repository.Budget{ShotCeiling: &ceiling}
	in := generation.RoutingInput{MinAcceptance: 75, Records: recordsOf(record(domain.ModelKling3, 9, 99, 1), record(domain.ModelSeedance2, 9, 60, 0.2))}

	routed, err := generation.DecideRouted(modeladapter.DefaultConfig(), domain.QualityBalanced, 5, budget, "", &in)
	require.NoError(t, err)
	plain, err := generation.Decide(modeladapter.DefaultConfig(), domain.QualityBalanced, 5, budget)
	require.NoError(t, err)

	assert.Equal(t, domain.ModelKling3, routed.Primary.Model)
	assert.Equal(t, 2, routed.Takes)
	assert.Equal(t, domain.ModelSeedance2, plain.Primary.Model)
	assert.Equal(t, 1, plain.Takes)
}
