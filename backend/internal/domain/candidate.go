package domain

// CandidateStrategy is the shooting strategy of one Prompt Candidate (spec/spec.md §8): the same
// Shot planned with a different priority, so that the Judge can compare the resulting Takes.
type CandidateStrategy string

// Candidate strategies, in the order the Generation Orchestrator tries them.
const (
	CandidateStabilityFirst CandidateStrategy = "stability_first"
	CandidateActingFirst    CandidateStrategy = "acting_first"
	CandidateDynamicCamera  CandidateStrategy = "dynamic_camera"
)

// CandidateStrategies lists every strategy in the order of spec/spec.md §8 (Candidate A, B, C).
func CandidateStrategies() []CandidateStrategy {
	return []CandidateStrategy{CandidateStabilityFirst, CandidateActingFirst, CandidateDynamicCamera}
}

// Label is the display name of the strategy.
func (s CandidateStrategy) Label() string {
	switch s {
	case CandidateStabilityFirst:
		return "Stability First"
	case CandidateActingFirst:
		return "Acting First"
	case CandidateDynamicCamera:
		return "Dynamic Camera"
	}
	return string(s)
}

// GenerationMode is how Prompt Candidates are generated (spec/spec.md §8 Generation Policy). It
// follows from the Quality Policy of the Project: MAX QUALITY generates for quality, FAST for cost.
type GenerationMode string

// Generation modes.
const (
	// ModeQuality generates every Candidate at the same time and lets the Judge pick the Best Take.
	ModeQuality GenerationMode = "quality"
	// ModeBalanced generates the first Candidate and adds the next one only while the Judge fails it.
	ModeBalanced GenerationMode = "balanced"
	// ModeEconomy generates as few Takes as possible and retakes only after a severe problem.
	ModeEconomy GenerationMode = "economy"
)

// Label is the display name of the mode.
func (m GenerationMode) Label() string {
	switch m {
	case ModeQuality:
		return "Quality Mode"
	case ModeBalanced:
		return "Balanced Mode"
	case ModeEconomy:
		return "Economy Mode"
	}
	return string(m)
}

// ModeOf is the Generation Mode of a Quality Policy. An unknown policy is treated as BALANCED,
// like EffectivePolicy does.
func ModeOf(q QualityPolicy) GenerationMode {
	switch q {
	case QualityMaxQuality:
		return ModeQuality
	case QualityFast:
		return ModeEconomy
	}
	return ModeBalanced
}
