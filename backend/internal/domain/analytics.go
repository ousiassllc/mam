package domain

// InterventionKind is what the user did by hand in the production (human_intervention.kind). The
// kinds and the rule for each Override are in spec/spec.md §28.3.
type InterventionKind string

// Intervention kinds.
const (
	// InterventionGenerate is the first generation of a Shot the user asked for.
	InterventionGenerate InterventionKind = "generate"
	// InterventionRegenerate is a generation of a Shot that already has Takes (a retake the user asked for).
	InterventionRegenerate InterventionKind = "regenerate"
	// InterventionRepair is a repair of a Take the user asked for.
	InterventionRepair InterventionKind = "repair"
	// InterventionSelectTake is the user's choice of a Take.
	InterventionSelectTake InterventionKind = "select_take"
	// InterventionRejectTake is the exclusion of a Take.
	InterventionRejectTake InterventionKind = "reject_take"
	// InterventionPartialTake is Partial Take Use.
	InterventionPartialTake InterventionKind = "partial_take"
	// InterventionEditShotSpec is a hand edit of a Canonical Shot Spec.
	InterventionEditShotSpec InterventionKind = "edit_shot_spec"
	// InterventionLockChange is a parameter set to MANUAL or LOCK.
	InterventionLockChange InterventionKind = "lock_change"
	// InterventionEditTimeline is a hand edit of the Edit Timeline.
	InterventionEditTimeline InterventionKind = "edit_timeline"
	// InterventionRequestChanges is a request for changes to an Approval card of a checkpoint.
	InterventionRequestChanges InterventionKind = "request_changes"
	// InterventionSwitchToDirector is the takeover of a Project: it switched to DIRECTOR MODE. It is
	// not a generation operation, so it is not a Human Intervention of the metrics; the AUTO
	// Completion Rate counts it.
	InterventionSwitchToDirector InterventionKind = "switch_to_director"
)

// Intervention is one row of human_intervention. ProjectID may be empty when ShotID or TakeID is
// set: the repository finds the Project from them.
type Intervention struct {
	ProjectID string
	ShotID    string
	TakeID    string
	Kind      InterventionKind
	// Override is true when the operation contradicts what the AI had decided.
	Override bool
}

// OverridesAIPick is true when choosing takeID contradicts the AI: a Take other than it is the AI
// Pick of the Shot. A Shot without an AI Pick has nothing to override.
func OverridesAIPick(takes []TakeInfo, takeID string) bool {
	for _, t := range takes {
		if t.Status == TakeAIPick && t.ID != takeID {
			return true
		}
	}
	return false
}

// Ratio is a metric of the MVP Success Metrics: Numerator / Denominator. A metric whose
// Denominator is 0 has nothing to measure yet and is not Defined (the screen shows "—", not 0%).
type Ratio struct {
	Numerator   float64
	Denominator float64
}

// Defined tells whether there is anything to divide by.
func (r Ratio) Defined() bool { return r.Denominator > 0 }

// Value is Numerator / Denominator, or 0 when the ratio is not Defined.
func (r Ratio) Value() float64 {
	if !r.Defined() {
		return 0
	}
	return r.Numerator / r.Denominator
}

// ProductionMetrics is the MVP Success Metrics (spec/spec.md §28) of one Project or of all of them.
// The formula of every field is in spec/spec.md §28.2.
type ProductionMetrics struct {
	// Counts the figures are taken from.
	Projects  int // Projects in scope
	Started   int // Projects past DRAFT
	Completed int // Projects that reached READY or EXPORTED, or have an Export (the finished videos)
	Exported  int // Projects with an Export
	Shots     int // Shots with at least one Take

	// Rates: Numerator / Denominator, shown as a percentage.
	FirstPassShotAcceptance  Ratio
	FinalShotAcceptance      Ratio
	CharacterContinuityFail  Ratio
	ProductAccuracyFail      Ratio
	PromptAdherence          Ratio
	AutoCompletion           Ratio
	UserOverride             Ratio
	ExportCompletion         Ratio
	NorthStar                Ratio
	AverageRetakesPerShot    Ratio // retakes / Shots
	InterventionsPerFinished Ratio // Human Interventions / finished videos
	CostPerAcceptedSecond    Ratio // cost (currency) / accepted seconds
	TimeToFirstRoughCut      Ratio // seconds / Projects with a Rough Cut
	TimeToFinalVideo         Ratio // seconds / Projects with an Export

	// OverrideEvents is the number of overrides recorded, Project-level ones (checkpoint changes,
	// Rough Cut edits) included; UserOverride counts Shots.
	OverrideEvents int
}

// The Judge dimensions each failure metric of §28 looks at. A Take fails a metric when its latest
// Judge result has an Issue of severity medium or high in one of them.
var (
	CharacterContinuityDimensions = []JudgeDimension{DimCharacterIdentity, DimCharacterConsistency, DimFace}
	ProductAccuracyDimensions     = []JudgeDimension{DimProductAccuracy, DimProductGeometry, DimLogoTextAccuracy, DimObjectConsistency}
	PromptAdherenceDimensions     = []JudgeDimension{DimPromptAdherence}
)
