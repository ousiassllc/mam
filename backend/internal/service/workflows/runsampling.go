package workflows

import (
	"math/rand/v2"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
)

// SeedMax is the largest seed a run takes: the largest whole number a JSON number holds exactly (what a client in JavaScript can send
// back).
const SeedMax = 1<<53 - 1

// WarnSampling is the Kind of a Warning about the detailed settings of a run (a value the workflow has no place for).
const WarnSampling = "sampling"

// runSettings is what a run takes of the seed and the sampling values of its input: the seed (the one named, else a new one, so that the
// record can say which it was), the sampling values the workflow takes, what the run uses (the values of the input where the workflow
// takes them, else the workflow's own: the record that makes the run repeatable) and the warning for the values it does not take.
type runSettings struct {
	seed     int64
	apply    sampling.Settings
	used     *sampling.Settings
	warnings []Warning
}

func settingsOf(saved localcfg.SavedWorkflow, in RunInput) (runSettings, error) {
	var s runSettings
	asked := in.Sampling
	asked.Sampler, asked.Scheduler = strings.TrimSpace(asked.Sampler), strings.TrimSpace(asked.Scheduler)
	if err := asked.Validate(); err != nil {
		return s, invalid("詳細設定が正しくありません: %v", err)
	}
	switch {
	case in.Seed == nil:
		s.seed = rand.Int64N(SeedMax + 1) //nolint:gosec // a sampler seed, not a credential
	case *in.Seed < 0 || *in.Seed > SeedMax:
		return s, invalid("seed は 0〜%d にしてください", int64(SeedMax))
	default:
		s.seed = *in.Seed
	}
	caps := saved.Model.Workflow.SamplingCaps()
	s.apply, s.used = caps.Applicable(asked), caps.Effective(asked)
	for _, sk := range caps.Skipped(asked) {
		why := "このワークフローには割り当てがありません"
		if sk.Reason == sampling.LockedBySpeedMode {
			why = "速さのモードが決めるため"
		}
		s.warnings = append(s.warnings, Warning{Kind: WarnSampling, Message: sk.Name + " は使いません（" + why + "）。ワークフローの値で実行します"})
	}
	return s, nil
}
