// Package generation is the Generation Orchestrator (spec/spec.md §11): it decides which model
// and strategy generate a Shot from the Shot Spec, World State, References, cost and model
// capabilities, runs Prompt Autopilot and the Model Adapter, and stores the resulting Takes.
// It also runs the Art Director to build the Reference Pack of a Shot (§10).
package generation

import (
	"errors"
	"fmt"
	"math"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ErrBudgetExceeded means not even one Take fits the budget ceiling of the Project or the Shot (F-22).
var ErrBudgetExceeded = errors.New("generation: the budget ceiling would be exceeded")

// Strategy is the generation plan of one Shot: the models to use and how many Takes to make.
type Strategy struct {
	Policy          domain.QualityPolicy
	Primary         modeladapter.VideoTarget
	Fallback        modeladapter.VideoTarget
	Takes           int
	DurationSeconds float64 // length of each clip, as the primary model produces it
	CostPerTake     float64 // estimated (USD)
	// Reasons lists, in order, what moved the plan away from the Quality Policy's default; it is
	// kept in prompt_version.model_adapter_output for traceability.
	Reasons []string
	// Routing is why these models were chosen (Multi-model routing); its Mode is empty when the
	// strategy was decided without it.
	Routing domain.RoutingDecision
}

// Decide chooses the strategy for a Shot of the given duration.
//
//   - The Quality Policy fixes the primary and alternative model, resolution and number of Takes.
//   - Model capabilities: when the Shot is longer than the longest clip of the primary model but
//     the alternative can hold it, the two swap (a longer clip is one Take, not a cut).
//   - Multi-model routing (DecideRouted) may replace the primary model on the record of the Judge.
//   - Cost: when the Takes would exceed the remaining budget of the Shot or the Project, the number
//     of Takes is reduced; when even one Take of the primary does not fit but one of the alternative
//     does, the alternative is used. Otherwise it fails with ErrBudgetExceeded.
func Decide(cfg modeladapter.Config, policy domain.QualityPolicy, shotSeconds float64, budget repository.Budget) (Strategy, error) {
	return DecideWithModel(cfg, policy, shotSeconds, budget, "")
}

// DecideWithModel is Decide for a Shot whose Video Model the user fixed (MANUAL, or LOCK): that
// model is the only one used, at the resolution of the Quality Policy, and is neither swapped for a
// longer-clip model nor replaced by the alternative when the budget is short. An empty forced
// is Decide.
func DecideWithModel(cfg modeladapter.Config, policy domain.QualityPolicy, shotSeconds float64, budget repository.Budget, forced domain.ModelName) (Strategy, error) {
	return DecideRouted(cfg, policy, shotSeconds, budget, forced, nil)
}

// DecideRouted is DecideWithModel with Multi-model routing (spec/architecture/model-adapters-m3.md
// §13): unless the model is forced or routing is switched off in cfg, the models of the Quality
// Policy are weighed against the other candidates by the nature of the Shot, the Judge record and
// the cost, before the budget is applied. A nil in leaves the routing out (and the decision unrecorded).
func DecideRouted(cfg modeladapter.Config, policy domain.QualityPolicy, shotSeconds float64, budget repository.Budget, forced domain.ModelName, in *RoutingInput) (Strategy, error) {
	pr, ok := cfg.Policies[policy]
	if !ok {
		return Strategy{}, fmt.Errorf("generation: no route for the quality policy %q", policy)
	}
	s := Strategy{Policy: policy, Primary: pr.Primary, Fallback: pr.Fallback, Takes: pr.Takes}
	decision := domain.RoutingDecision{
		PolicyPrimary:  domain.RoutingModel{Model: pr.Primary.Model, Resolution: pr.Primary.Resolution},
		PolicyFallback: domain.RoutingModel{Model: pr.Fallback.Model, Resolution: pr.Fallback.Resolution},
	}

	switch {
	case forced != "":
		s.Primary = modeladapter.VideoTarget{Model: forced, Resolution: pr.Primary.Resolution}
		s.Fallback = s.Primary
		s.Reasons = append(s.Reasons, fmt.Sprintf("the video model is fixed by the user: %s", forced))
		decision.Mode, decision.Reason = domain.RoutingFixed, fmt.Sprintf("the video model is fixed by the user: %s", forced)
	default:
		if longest(pr.Primary.Model) < shotSeconds && longest(pr.Fallback.Model) >= shotSeconds {
			s.Primary, s.Fallback = s.Fallback, s.Primary
			s.Reasons = append(s.Reasons, fmt.Sprintf("the %.1fs Shot exceeds the longest clip of %s: %s is primary", shotSeconds, pr.Primary.Model, s.Primary.Model))
		}
		switch {
		case in == nil:
		case !cfg.Routing.Enabled:
			decision.Mode, decision.Reason = domain.RoutingDisabled, "Multi-model routing is switched off: the Quality Policy's assignment is used"
		default:
			s = route(cfg, policy, shotSeconds, s, *in, &decision)
		}
	}

	remaining := budget.Remaining()
	cost := func(t modeladapter.VideoTarget) float64 {
		caps, _ := modeladapter.Capabilities(t.Model)
		return cfg.VideoCost(t, caps.SnapDuration(shotSeconds))
	}
	perTake := cost(s.Primary)
	if fit := affordable(remaining, perTake); fit < s.Takes {
		if fit == 0 {
			if alt := cost(s.Fallback); s.Fallback != s.Primary && affordable(remaining, alt) > 0 {
				s.Reasons = append(s.Reasons, fmt.Sprintf("%s does not fit the budget: %s is primary", s.Primary.Model, s.Fallback.Model))
				s.Primary, s.Fallback = s.Fallback, s.Primary
				perTake = alt
				fit = affordable(remaining, perTake)
			}
		}
		if fit == 0 {
			return Strategy{}, ErrBudgetExceeded
		}
		s.Reasons = append(s.Reasons, fmt.Sprintf("the budget allows %d of %d Takes", min(fit, s.Takes), s.Takes))
		s.Takes = min(fit, s.Takes)
	}
	caps, _ := modeladapter.Capabilities(s.Primary.Model)
	s.DurationSeconds = caps.SnapDuration(shotSeconds)
	s.CostPerTake = perTake
	if decision.Mode != "" {
		decision.Primary = domain.RoutingModel{Model: s.Primary.Model, Resolution: s.Primary.Resolution}
		decision.Fallback = domain.RoutingModel{Model: s.Fallback.Model, Resolution: s.Fallback.Resolution}
		s.Routing = decision
	}
	return s, nil
}

// routing is the decision to record with a Take; nil when the strategy was decided without routing.
func (s Strategy) routing() *domain.RoutingDecision {
	if s.Routing.Mode == "" {
		return nil
	}
	return &s.Routing
}

func longest(m domain.ModelName) float64 {
	caps, ok := modeladapter.Capabilities(m)
	if !ok || len(caps.Durations) == 0 {
		return 0
	}
	return caps.Durations[len(caps.Durations)-1]
}

// affordable is how many Takes of the given cost fit into remaining.
func affordable(remaining, perTake float64) int {
	if math.IsInf(remaining, 1) || perTake <= 0 {
		return math.MaxInt32
	}
	if remaining < perTake {
		return 0
	}
	return int(math.Floor(remaining/perTake + 1e-9))
}
