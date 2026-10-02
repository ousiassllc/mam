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
	StateAudio:            {StateFinalQC, StateEditing}, // EDITING: the user changed the edit once the audio was being made
	StateFinalQC:          {StatePlanning, StateVisualDesign, StateGenerating, StateEditing, StateAudio, StateReady, StateAwaitingApproval},
	StateReady:            {StateExported},
}

// CanTransition reports whether the Core State Machine may move from one state to another.
func CanTransition(from, to ProjectState) bool {
	return slices.Contains(stateTransitions[from], to)
}

// CanAwaitApproval reports whether a card that blocks the Project (an Escalation) can put it in
// AWAITING_APPROVAL: the Core State Machine has no such move from DRAFT, AUDIO, READY or EXPORTED.
func (s ProjectState) CanAwaitApproval() bool { return CanTransition(s, StateAwaitingApproval) }

// CanEdit reports whether a Project in the state can take an edit: a Rough Cut or a change of the
// Edit Timeline moves it to EDITING (EditorService.toEditing) from these states and is refused (409)
// from any other (a card waits, or it is DRAFT / PLANNING / READY / EXPORTED). The editor's
// read-only flag and the Rough Cut button are drawn from the same answer.
func (s ProjectState) CanEdit() bool {
	switch s {
	case StateEditing, StateVisualDesign, StateGenerating, StateJudging, StateRepairing, StateEditJudging, StateAudio, StateFinalQC:
		return true
	}
	return false
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

// Busy reports whether a generation or a repair is writing the Shot: a start of either is refused
// (409) while it is. The Shot card draws the buttons that start one only when it is not.
func (s ShotState) Busy() bool { return s == ShotGenerating || s == ShotRepairing }

// CanEditSpec reports whether the Shot's spec may be saved by hand or restored from a version: the
// generation and the Repair write the spec at their end and would overwrite the edit, so both are
// refused (409) while the Shot is Busy. The Shot Inspector draws the spec form and the restore buttons
// only when it holds; the repositories refuse with the same predicate.
func (s ShotState) CanEditSpec() bool { return !s.Busy() }

// CanPinReference reports whether a Reference Library image may be added to the Shot's Reference Pack:
// the pack is read by the generation and the Judge, so it is refused (409) while the Shot is being
// generated or judged. The Shot Inspector draws the form only when it holds; the repository refuses
// with the same predicate.
func (s ShotState) CanPinReference() bool { return s != ShotGenerating && s != ShotJudging }

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

// Valid reports whether m is a known mode.
func (m Mode) Valid() bool { return m == ModeAuto || m == ModeDirector }

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
