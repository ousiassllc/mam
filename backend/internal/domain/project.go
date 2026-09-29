package domain

import "slices"

// ProjectState is a state of the Core State Machine (spec/spec.md §22).
type ProjectState string

// Core State Machine states. The same set is enforced by the CHECK constraint on project.state.
const (
	StateDraft            ProjectState = "DRAFT"
	StatePlanning         ProjectState = "PLANNING"
	StateVisualDesign     ProjectState = "VISUAL_DESIGN"
	StateAwaitingApproval ProjectState = "AWAITING_APPROVAL"
	StateGenerating       ProjectState = "GENERATING"
	StateJudging          ProjectState = "JUDGING"
	StateRepairing        ProjectState = "REPAIRING"
	StateEditing          ProjectState = "EDITING"
	StateEditJudging      ProjectState = "EDIT_JUDGING"
	StateAudio            ProjectState = "AUDIO"
	StateFinalQC          ProjectState = "FINAL_QC"
	StateReady            ProjectState = "READY"
	StateExported         ProjectState = "EXPORTED"
)

// ProjectStates lists every valid ProjectState in state-machine order.
func ProjectStates() []ProjectState {
	return []ProjectState{
		StateDraft, StatePlanning, StateVisualDesign, StateAwaitingApproval,
		StateGenerating, StateJudging, StateRepairing, StateEditing,
		StateEditJudging, StateAudio, StateFinalQC, StateReady, StateExported,
	}
}

// stateTransitions lists the legal moves of the Core State Machine (spec/spec.md §22).
// AWAITING_APPROVAL is entered from every stage that has a checkpoint or an escalation
// and left to the state chosen by Checkpoint.ApprovedState / ChangesRequestedState or
// EscalationResumeState.
var stateTransitions = map[ProjectState][]ProjectState{
	StateDraft:            {StatePlanning},
	StatePlanning:         {StateVisualDesign, StateAwaitingApproval},
	StateVisualDesign:     {StateGenerating, StateAwaitingApproval},
	StateAwaitingApproval: {StatePlanning, StateVisualDesign, StateGenerating, StateRepairing, StateEditing, StateAudio, StateReady},
	StateGenerating:       {StateJudging, StateAwaitingApproval},
	StateJudging:          {StateRepairing, StateEditing, StateAwaitingApproval},
	StateRepairing:        {StateGenerating, StateAwaitingApproval},
	StateEditing:          {StateEditJudging, StateAwaitingApproval},
	StateEditJudging:      {StatePlanning, StateGenerating, StateEditing, StateAudio, StateAwaitingApproval},
	StateAudio:            {StateFinalQC},
	StateFinalQC:          {StatePlanning, StateVisualDesign, StateGenerating, StateEditing, StateAudio, StateReady, StateAwaitingApproval},
	StateReady:            {StateExported},
}

// CanTransition reports whether the Core State Machine may move from one state to another.
func CanTransition(from, to ProjectState) bool {
	return slices.Contains(stateTransitions[from], to)
}

// ShotState is the sub-state of a Shot within the Core State Machine.
type ShotState string

// Shot sub-states. ShotEditing means the Shot passed judging and waits for editing.
const (
	ShotPlanning   ShotState = "PLANNING"
	ShotGenerating ShotState = "GENERATING"
	ShotJudging    ShotState = "JUDGING"
	ShotRepairing  ShotState = "REPAIRING"
	ShotEditing    ShotState = "EDITING"
)

// ShotStates lists every valid ShotState.
func ShotStates() []ShotState {
	return []ShotState{ShotPlanning, ShotGenerating, ShotJudging, ShotRepairing, ShotEditing}
}

// Mode is the AUTO / DIRECTOR switch (F-02).
type Mode string

// Project modes.
const (
	ModeAuto     Mode = "auto"
	ModeDirector Mode = "director"
)

// ConfirmationLevel is the human-confirmation policy (F-03).
type ConfirmationLevel string

// Confirmation levels.
const (
	ConfirmFullAuto    ConfirmationLevel = "full_auto"
	ConfirmKeyCheck    ConfirmationLevel = "key_check"
	ConfirmCustomCheck ConfirmationLevel = "custom_check"
)

// ProductionTemplate selects the evaluation axes (F-21).
type ProductionTemplate string

// Production templates.
const (
	TemplateProductCM ProductionTemplate = "product_cm"
	TemplateDrama     ProductionTemplate = "drama"
	TemplateAnime     ProductionTemplate = "anime"
	TemplateSNSShort  ProductionTemplate = "sns_short"
)

// QualityPolicy is the quality/cost trade-off (F-22).
type QualityPolicy string

// Quality policies.
const (
	QualityFast       QualityPolicy = "fast"
	QualityBalanced   QualityPolicy = "balanced"
	QualityMaxQuality QualityPolicy = "max_quality"
)
