package generation

import (
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/generation/axisweight"
	"github.com/ousiassllc/mam/backend/internal/agent/generation/refimage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// RoutingInput is what Multi-model routing weighs besides the configuration
// (spec/architecture/model-adapters-m3.md §13): the nature of the Shot, the Judge record of each video model in the
// Project, the Project's Minimum acceptance score and the state of the vendors.
type RoutingInput struct {
	// Spec is the Shot Spec the Shot is generated from; nil when the Shot has none yet.
	Spec       *domain.ShotSpec
	References []domain.Reference
	// Audio is the sound of a Shot with audio (Issue #1153; nil: none). The Shot's models were chosen for it (modelroute), so they are not weighed (DecideRouted), and the clip is as long as it.
	Audio *refimage.Audio
	// Records are the Judge records of the Project by video model.
	Records map[domain.ModelName]domain.ModelRecord
	// MinAcceptance is the Minimum acceptance score (0-100) of the Quality/Cost Policy.
	MinAcceptance int
	// Unavailable says why a model cannot be called now ("" when it can). nil means every model can.
	Unavailable func(domain.ModelName) string
}

// hasKeyframe reports whether the references hold a Keyframe: the first frame of the clip (decide passes only a Keyframe that is read, #902).
func hasKeyframe(refs []domain.Reference) bool {
	return slices.ContainsFunc(refs, func(r domain.Reference) bool { return r.Kind == domain.RefKeyframe })
}

// fit is the share of what the Shot asks of a video model that the model can do (§12.3): to hold the
// Shot in one clip, to take a first frame when the Reference Pack has a Keyframe, and to take free
// reference images when it has other references. It is 1 when the Shot asks for nothing.
func fit(cfg modeladapter.Config, m domain.ModelName, seconds float64, refs []domain.Reference) float64 {
	caps, ok := cfg.VideoCaps(m)
	if !ok {
		return 0
	}
	asked, met := 1, 0
	if longest(cfg, m) >= seconds-1e-9 {
		met++
	}
	if hasKeyframe(refs) {
		asked++
		if caps.FirstFrame {
			met++
		}
	}
	if slices.ContainsFunc(refs, func(r domain.Reference) bool { return r.Kind != domain.RefKeyframe }) {
		asked++
		if caps.MaxReferenceImages > 0 {
			met++
		}
	}
	return float64(met) / float64(asked)
}

// route applies Multi-model routing to the strategy the Quality Policy gave (§12.3, §12.4): a
// challenger replaces the primary model when it has a Judge record of min_samples Takes, reaches the
// Minimum acceptance score, is callable, and scores better than the primary by the switch margin.
// The decision d records why; the previous primary becomes the alternative on a switch.
func route(cfg modeladapter.Config, policy domain.QualityPolicy, seconds float64, s Strategy, in RoutingInput, d *domain.RoutingDecision) Strategy {
	rc := cfg.Routing
	weights, axis := rc.Weights[policy], axisweight.Of(in.Spec)

	// Candidates: the primary (anchor) first, then the others of the policy, one per model.
	targets := []modeladapter.VideoTarget{s.Primary}
	for _, t := range rc.Candidates[policy] {
		if !slices.ContainsFunc(targets, func(o modeladapter.VideoTarget) bool { return o.Model == t.Model }) {
			targets = append(targets, t)
		}
	}
	cheapest := 0.0
	costs := make([]float64, len(targets))
	for i, t := range targets {
		costs[i] = cfg.VideoCost(t, cfg.ClipSeconds(t.Model, seconds, hasKeyframe(in.References)))
		if costs[i] > 0 && (cheapest == 0 || costs[i] < cheapest) {
			cheapest = costs[i]
		}
	}
	cands := make([]domain.RoutingCandidate, len(targets))
	for i, t := range targets {
		rec := in.Records[t.Model]
		c := domain.RoutingCandidate{
			Model: t.Model, Resolution: t.Resolution, Anchor: i == 0, Samples: rec.Samples, CostPerTake: costs[i],
			Quality: rc.PriorQuality, Cost: 1, Fit: fit(cfg, t.Model, seconds, in.References),
		}
		if rec.Samples > 0 {
			c.MeanScore, c.PassRate = axisweight.Score(rec.Scores, axis), rec.PassRate
		}
		if rec.Samples >= rc.MinSamples {
			c.Quality = 0.5*c.MeanScore/domain.MaxScore + 0.5*c.PassRate
		}
		if costs[i] > 0 {
			c.Cost = cheapest / costs[i]
		}
		c.Score = weights.Quality*c.Quality + weights.Cost*c.Cost + weights.Fit*c.Fit
		cands[i] = c
	}

	best := -1
	for i := 1; i < len(cands); i++ {
		c := &cands[i]
		switch {
		case in.Unavailable != nil && in.Unavailable(c.Model) != "":
			c.Excluded = in.Unavailable(c.Model)
		case c.Samples < rc.MinSamples:
			c.Excluded = domain.ExcludedInsufficientEvidence
		case c.MeanScore < float64(in.MinAcceptance):
			c.Excluded = domain.ExcludedBelowMinimumScore
		case c.Score-cands[0].Score < rc.SwitchMargin:
			c.Excluded = domain.ExcludedMargin
		case best < 0 || c.Score > cands[best].Score:
			if best >= 0 {
				cands[best].Excluded = domain.ExcludedOutscored
			}
			best = i
		default:
			c.Excluded = domain.ExcludedOutscored
		}
	}

	d.Mode, d.AxisWeights, d.Candidates = domain.RoutingPolicyDefault, &axis, cands
	if best < 0 {
		d.Reason = policyKept(cands, rc.MinSamples, s.Primary.Model)
		return s
	}
	c := cands[best]
	d.Mode = domain.RoutingAdaptive
	d.Reason = fmt.Sprintf("%s (%s) replaces %s as the primary model: score %.2f against %.2f (Judge record: %d Takes, mean %.0f, %.0f%% passed)",
		c.Model, c.Resolution, s.Primary.Model, c.Score, cands[0].Score, c.Samples, c.MeanScore, c.PassRate*100)
	s.Fallback, s.Primary = s.Primary, targets[best]
	s.Reasons = append(s.Reasons, "multi-model routing: "+d.Reason)
	return s
}

// policyKept says in a sentence why no challenger replaced the primary model.
func policyKept(cands []domain.RoutingCandidate, minSamples int, primary domain.ModelName) string {
	for _, c := range cands[1:] {
		if c.Excluded == domain.ExcludedBelowMinimumScore || c.Excluded == domain.ExcludedMargin {
			return fmt.Sprintf("no other video model scored better than %s by the switch margin while reaching the minimum acceptance score: the Quality Policy's assignment is kept", primary)
		}
	}
	return fmt.Sprintf("no other video model has a Judge record of %d Takes in this Project and can be called: the Quality Policy's assignment is kept", minSamples)
}
