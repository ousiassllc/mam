package domain

import (
	"encoding/json"
	"time"
)

// JudgeVerdict is judge_result.result.
type JudgeVerdict string

// Judge verdicts.
const (
	JudgePass JudgeVerdict = "pass"
	JudgeFail JudgeVerdict = "fail"
)

// IssueSeverity is how bad a Judge Issue is.
type IssueSeverity string

// Issue severities.
const (
	SeverityLow    IssueSeverity = "low"
	SeverityMedium IssueSeverity = "medium"
	SeverityHigh   IssueSeverity = "high"
)

// Valid reports whether s is a known severity.
func (s IssueSeverity) Valid() bool {
	return s == SeverityLow || s == SeverityMedium || s == SeverityHigh
}

// Rank orders severities (higher is worse); an unknown severity ranks 0.
func (s IssueSeverity) Rank() int {
	switch s {
	case SeverityLow:
		return 1
	case SeverityMedium:
		return 2
	case SeverityHigh:
		return 3
	}
	return 0
}

// IssueTypeFaceIdentityDrift is the Issue the local Face Identity Similarity measurement raises for
// a run of frames whose similarity to the reference face fell below a threshold (spec/spec.md §12.4).
const IssueTypeFaceIdentityDrift = "face_identity_drift"

// JudgeDimension is an evaluation dimension of the AI Video Judge (spec/spec.md §12.2).
type JudgeDimension string

// Evaluation dimensions. DimFaceIdentity covers the Character Identity / Face pair.
const (
	DimPromptAdherence      JudgeDimension = "prompt_adherence"
	DimCharacterIdentity    JudgeDimension = "character_identity"
	DimCharacterConsistency JudgeDimension = "character_consistency"
	DimProductAccuracy      JudgeDimension = "product_accuracy"
	DimProductGeometry      JudgeDimension = "product_geometry"
	DimLogoTextAccuracy     JudgeDimension = "logo_text_accuracy"
	DimObjectConsistency    JudgeDimension = "object_consistency"
	DimFace                 JudgeDimension = "face"
	DimHands                JudgeDimension = "hands"
	DimPose                 JudgeDimension = "pose"
	DimActing               JudgeDimension = "acting"
	DimLipSync              JudgeDimension = "lip_sync"
	DimMotionQuality        JudgeDimension = "motion_quality"
	DimPhysics              JudgeDimension = "physics"
	DimCameraMotion         JudgeDimension = "camera_motion"
	DimComposition          JudgeDimension = "composition"
	DimLighting             JudgeDimension = "lighting"
	DimBackground           JudgeDimension = "background_consistency"
	DimTemporal             JudgeDimension = "temporal_consistency"
	DimFlicker              JudgeDimension = "flicker"
	DimSceneContinuity      JudgeDimension = "scene_continuity"
	DimShotCompatibility    JudgeDimension = "shot_compatibility"
)

// JudgeDimensions lists every dimension in the order of spec/spec.md §12.2.
func JudgeDimensions() []JudgeDimension {
	return []JudgeDimension{
		DimPromptAdherence, DimCharacterIdentity, DimCharacterConsistency, DimProductAccuracy, DimProductGeometry,
		DimLogoTextAccuracy, DimObjectConsistency, DimFace, DimHands, DimPose, DimActing, DimLipSync,
		DimMotionQuality, DimPhysics, DimCameraMotion, DimComposition, DimLighting, DimBackground,
		DimTemporal, DimFlicker, DimSceneContinuity, DimShotCompatibility,
	}
}

// Valid reports whether d is a known dimension.
func (d JudgeDimension) Valid() bool {
	for _, v := range JudgeDimensions() {
		if v == d {
			return true
		}
	}
	return false
}

// JudgeIssue is one problem of a Take at a time range. TimeRange is "2.1-2.8s"; Frames is the
// first and last source frame number of the range.
type JudgeIssue struct {
	Type        string         `json:"type"`
	Severity    IssueSeverity  `json:"severity"`
	Dimension   JudgeDimension `json:"dimension,omitempty"`
	TimeRange   string         `json:"time_range,omitempty"`
	Frames      []int          `json:"frames,omitempty"`
	CharacterID string         `json:"character_id,omitempty"`
	Description string         `json:"description,omitempty"`
	// LockViolation is set when the Issue breaks a LOCKed parameter (Character Identity, Product
	// Geometry): such a Take fails whatever its score is (spec/spec.md §13).
	LockViolation bool `json:"lock_violation,omitempty"`
}

// FaceIdentityStatus says how the numeric Face Identity Similarity ended.
type FaceIdentityStatus string

// Face Identity Similarity statuses (metrics.face_identity_status).
const (
	// FaceMeasured: at least one Character was measured (see each entry's Evaluated).
	FaceMeasured FaceIdentityStatus = "measured"
	// FaceNotApplicable: no Character with a face reference takes part in the Shot.
	FaceNotApplicable FaceIdentityStatus = "not_applicable"
	// FaceUnavailable: the local face models are not installed; the Take was judged qualitatively only.
	FaceUnavailable FaceIdentityStatus = "unavailable"
)

// FaceIdentityMetric is the Face Identity Similarity of one Character in a Take (spec/spec.md §12.3).
// Similarities are cosine similarities to the Character's reference faces. Frames without a
// detectable face are missing values: they count in UndetectedRatio only. Evaluated is false when
// too few frames had a face for the numbers to mean anything; the Take is then judged by the
// qualitative evaluation alone.
type FaceIdentityMetric struct {
	CharacterID     string  `json:"character_id"`
	SimilarityMean  float64 `json:"similarity_mean"`
	SimilarityMin   float64 `json:"similarity_min"`
	SampledFrames   int     `json:"sampled_frames"`
	UndetectedRatio float64 `json:"undetected_ratio"`
	Evaluated       bool    `json:"evaluated"`
	// WarnThreshold / FailThreshold are the thresholds the measurement used; Calibrated tells that
	// they were derived from the similarity of the Character's own reference images.
	WarnThreshold float64 `json:"warn_threshold"`
	FailThreshold float64 `json:"fail_threshold"`
	Calibrated    bool    `json:"calibrated"`
}

// JudgeMetrics are the numeric measurements of a Judge run (judge_result.metrics).
type JudgeMetrics struct {
	FaceIdentity       []FaceIdentityMetric `json:"face_identity,omitempty"`
	FaceIdentityStatus FaceIdentityStatus   `json:"face_identity_status,omitempty"`
}

// RepairAction is a repair strategy of the Repair / Retake Loop (spec/spec.md §13).
type RepairAction string

// Repair actions.
const (
	RepairPrompt              RepairAction = "prompt_repair"
	RepairPartialRegeneration RepairAction = "partial_regeneration"
	RepairFullRetake          RepairAction = "full_retake"
	RepairDifferentModel      RepairAction = "different_model"
	RepairReferenceReinforce  RepairAction = "reference_reinforcement"
	RepairEditLevel           RepairAction = "edit_level_repair"
)

// InstructionIncreaseFaceReference is the repair instruction that goes with face_identity_drift.
const InstructionIncreaseFaceReference = "increase_face_reference_strength"

// RepairActions lists every repair action.
func RepairActions() []RepairAction {
	return []RepairAction{
		RepairPrompt, RepairPartialRegeneration, RepairFullRetake, RepairDifferentModel, RepairReferenceReinforce, RepairEditLevel,
	}
}

// Valid reports whether a is a known repair action.
func (a RepairAction) Valid() bool {
	for _, v := range RepairActions() {
		if v == a {
			return true
		}
	}
	return false
}

// RepairStrategy is what the Judge recommends to the Repair Agent (judge_result.repair_strategy).
type RepairStrategy struct {
	Action       RepairAction `json:"action"`
	Instructions []string     `json:"instructions,omitempty"`
}

// JudgeResult is one run of the AI Video Judge on a Take (spec/spec.md §12.3). Every run is kept
// (judge_result.take_id is not unique); the latest one is the current verdict.
type JudgeResult struct {
	ID                string          `json:"-"`
	TakeID            string          `json:"-"`
	Result            JudgeVerdict    `json:"result"`
	OverallConfidence float64         `json:"overall_confidence"`
	Metrics           JudgeMetrics    `json:"metrics"`
	Issues            []JudgeIssue    `json:"issues"`
	Preserve          []string        `json:"preserve"`
	RepairStrategy    *RepairStrategy `json:"repair_strategy,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
}

// EncodeJSON returns the columns of judge_result as JSON: issues, metrics, preserve, repair strategy.
func (r JudgeResult) EncodeJSON() (issues, metrics, preserve, repair []byte, err error) {
	if issues, err = json.Marshal(nonNil(r.Issues)); err != nil {
		return
	}
	if metrics, err = json.Marshal(r.Metrics); err != nil {
		return
	}
	if preserve, err = json.Marshal(nonNil(r.Preserve)); err != nil {
		return
	}
	repair, err = json.Marshal(r.RepairStrategy) // "null" when there is none
	return
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// LockKey names one LOCKed parameter (lock.target_type, lock.target_id, lock.parameter_name).
type LockKey struct {
	TargetType string
	TargetID   string
	Parameter  string
}

// Lock targets and parameters the AI Video Judge checks (spec/spec.md §6, §13).
const (
	LockTargetCharacter = "character"
	LockTargetProduct   = "product"
	// LockParamIdentity is the Character Identity: the face and look of the Character.
	LockParamIdentity = "identity"
	// LockParamGeometry is the Product Geometry: the shape of the Product.
	LockParamGeometry = "geometry"
	// LockParamLabel is the Product's logo and text.
	LockParamLabel = "label"
)
