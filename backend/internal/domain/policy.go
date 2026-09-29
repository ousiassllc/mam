package domain

import (
	"slices"
	"time"
)

// QualityPolicies lists the Quality Policies from the cheapest to the most thorough.
func QualityPolicies() []QualityPolicy {
	return []QualityPolicy{QualityFast, QualityBalanced, QualityMaxQuality}
}

// Valid reports whether q is one of the three Quality Policies.
func (q QualityPolicy) Valid() bool { return slices.Contains(QualityPolicies(), q) }

// Label is the display name of the policy.
func (q QualityPolicy) Label() string {
	switch q {
	case QualityFast:
		return "FAST"
	case QualityBalanced:
		return "BALANCED"
	case QualityMaxQuality:
		return "MAX QUALITY"
	}
	return string(q)
}

// Bounds of the individual settings of the Quality/Cost Policy (UC-8).
const (
	MaxRetriesLimit           = 10
	MinGenerationSeconds      = 10
	MaxGenerationSecondsLimit = 3600
	MaxScore                  = 100
)

// PolicyLimits are the settings the user may override on top of the Quality Policy (UC-8). A nil
// field means "use the default of the Quality Policy".
type PolicyLimits struct {
	// MaxRetries is the number of retries (Repair / Retake) of one Shot after its first generation.
	MaxRetries *int
	// MaxGenerationSeconds bounds the time spent on generating one Take.
	MaxGenerationSeconds *int
	// MinAcceptanceScore is the overall score (0-100, weighted by the Production Template) a Take needs to pass.
	MinAcceptanceScore *int
	// MinAxisScore is the score (0-100) every single axis of the Production Template needs to reach.
	MinAxisScore *int
}

// qualityDefaults are the limits every Quality Policy applies until the user overrides them.
var qualityDefaults = map[QualityPolicy]struct {
	retries, seconds, acceptance, axis int
}{
	QualityFast:       {retries: 1, seconds: 180, acceptance: 60, axis: 40},
	QualityBalanced:   {retries: 2, seconds: 300, acceptance: 75, axis: 55},
	QualityMaxQuality: {retries: 4, seconds: 600, acceptance: 85, axis: 70},
}

// EffectivePolicy is what the Generation Orchestrator and the Repair / Retake Loop obey: the
// Quality Policy with the user's overrides applied (spec/spec.md §13 Retry Policy, §25).
type EffectivePolicy struct {
	Quality QualityPolicy
	// ProjectBudgetCeiling and ShotBudgetCeiling are in USD; nil means no ceiling. The Shot ceiling is
	// also the "Max cost per Shot" of the Retry Policy.
	ProjectBudgetCeiling, ShotBudgetCeiling *float64
	MaxRetries                              int
	MaxGenerationTime                       time.Duration
	MinAcceptanceScore, MinAxisScore        int
}

// ProjectPolicy is the Production Template and the Quality/Cost Policy of a Project as stored.
type ProjectPolicy struct {
	ProjectID string
	Template  ProductionTemplate
	Quality   QualityPolicy
	// ProjectBudgetCeiling and ShotBudgetCeiling are in USD; nil means no ceiling.
	ProjectBudgetCeiling, ShotBudgetCeiling *float64
	Limits                                  PolicyLimits
}

// Effective resolves the overrides against the defaults of the Quality Policy.
func (p ProjectPolicy) Effective() EffectivePolicy {
	d, ok := qualityDefaults[p.Quality]
	if !ok {
		d = qualityDefaults[QualityBalanced]
	}
	pick := func(v *int, def int) int {
		if v != nil {
			return *v
		}
		return def
	}
	return EffectivePolicy{
		Quality:              p.Quality,
		ProjectBudgetCeiling: p.ProjectBudgetCeiling, ShotBudgetCeiling: p.ShotBudgetCeiling,
		MaxRetries:         pick(p.Limits.MaxRetries, d.retries),
		MaxGenerationTime:  time.Duration(pick(p.Limits.MaxGenerationSeconds, d.seconds)) * time.Second,
		MinAcceptanceScore: pick(p.Limits.MinAcceptanceScore, d.acceptance),
		MinAxisScore:       pick(p.Limits.MinAxisScore, d.axis),
	}
}

// Accepts reports whether an evaluation passes the Minimum quality thresholds: the overall score
// reaches MinAcceptanceScore and every axis score reaches MinAxisScore. A LOCK violation fails
// regardless of the score; the Judge decides that before it asks.
func (p EffectivePolicy) Accepts(overall float64, axes map[EvaluationAxis]float64) bool {
	if overall < float64(p.MinAcceptanceScore) {
		return false
	}
	for _, s := range axes {
		if s < float64(p.MinAxisScore) {
			return false
		}
	}
	return true
}

// RetryVerdict is the answer of the Retry Policy to "may this Shot be generated again?".
type RetryVerdict string

// Retry verdicts. Everything but RetryAllowed means the loop stops and escalates to the user.
const (
	RetryAllowed       RetryVerdict = "allowed"
	RetryMaxRetries    RetryVerdict = "max_retries"    // Max retries per Shot reached
	RetryShotBudget    RetryVerdict = "shot_budget"    // Max cost per Shot would be exceeded
	RetryProjectBudget RetryVerdict = "project_budget" // Project budget ceiling would be exceeded
)

// CanRetry applies the Retry Policy (spec/spec.md §13): retriesDone is the number of retries the
// Shot has already had, shotSpent and projectSpent are what the cost ledger holds, and nextCost is the
// estimated cost of the retry.
func (p EffectivePolicy) CanRetry(retriesDone int, shotSpent, projectSpent, nextCost float64) RetryVerdict {
	switch {
	case retriesDone >= p.MaxRetries:
		return RetryMaxRetries
	case p.ShotBudgetCeiling != nil && shotSpent+nextCost > *p.ShotBudgetCeiling:
		return RetryShotBudget
	case p.ProjectBudgetCeiling != nil && projectSpent+nextCost > *p.ProjectBudgetCeiling:
		return RetryProjectBudget
	}
	return RetryAllowed
}
