package domain

import "slices"

// Checkpoint is a point of the pipeline where a human may be asked to confirm
// (spec/spec.md §4.4). Whether an approval card is shown depends on the
// Project's ConfirmationLevel.
type Checkpoint string

// Checkpoints.
const (
	CheckpointScript           Checkpoint = "script"             // Concept / Script
	CheckpointCharacterDesign  Checkpoint = "character_design"   // Character design
	CheckpointMainVisual       Checkpoint = "main_visual"        // Main Visual / Character / Product
	CheckpointProductKeyVisual Checkpoint = "product_key_visual" // Product key visual
	CheckpointSceneComplete    Checkpoint = "scene_complete"     // Each Scene when it is completed
	CheckpointRoughCut         Checkpoint = "rough_cut"
	CheckpointFinal            Checkpoint = "final"
)

// Checkpoints lists every checkpoint in pipeline order.
func Checkpoints() []Checkpoint {
	return []Checkpoint{
		CheckpointScript, CheckpointCharacterDesign, CheckpointMainVisual,
		CheckpointProductKeyVisual, CheckpointSceneComplete, CheckpointRoughCut, CheckpointFinal,
	}
}

// SelectableCheckpoints are the checkpoints the pipeline actually posts an Approval card at:
// the script (Orchestrator), the Rough Cut and the Final (Editor). Main Visual, character
// design, product key visual and Scene completion belong to the stages after the Scene/Shot
// plan, which are started per Shot by hand, so nothing asks there (spec/spec.md §4.4). Only
// the selectable ones are offered for CUSTOM CHECK.
func SelectableCheckpoints() []Checkpoint {
	return []Checkpoint{CheckpointScript, CheckpointRoughCut, CheckpointFinal}
}

// KeyCheckpoints are the standard points of KEY CHECK: Concept/Script, Rough Cut and Final
// (spec/spec.md §4.4).
func KeyCheckpoints() []Checkpoint {
	return []Checkpoint{CheckpointScript, CheckpointRoughCut, CheckpointFinal}
}

// Selectable reports whether c can be chosen for CUSTOM CHECK (see SelectableCheckpoints).
func (c Checkpoint) Selectable() bool { return slices.Contains(SelectableCheckpoints(), c) }

// Valid reports whether c is a known checkpoint.
func (c Checkpoint) Valid() bool { return slices.Contains(Checkpoints(), c) }

// Label is the Japanese display name.
func (c Checkpoint) Label() string {
	switch c {
	case CheckpointScript:
		return "コンセプト / 脚本"
	case CheckpointCharacterDesign:
		return "キャラクターデザイン"
	case CheckpointMainVisual:
		return "Main Visual / キャラクター / 商品"
	case CheckpointProductKeyVisual:
		return "商品 Key Visual"
	case CheckpointSceneComplete:
		return "Scene 完成"
	case CheckpointRoughCut:
		return "Rough Cut"
	case CheckpointFinal:
		return "Final"
	}
	return string(c)
}

// RefType is the chat_message.ref_type of the approval card of this checkpoint:
// the type of the artifact being approved (er.md §4).
func (c Checkpoint) RefType() string {
	switch c {
	case CheckpointScript:
		return "script_version"
	case CheckpointSceneComplete:
		return "scene"
	case CheckpointRoughCut:
		return "edit_timeline_version"
	case CheckpointFinal:
		return "export"
	}
	return string(c) // character_design, main_visual, product_key_visual
}

// CheckpointForRefType is the inverse of Checkpoint.RefType.
func CheckpointForRefType(refType string) (Checkpoint, bool) {
	for _, c := range Checkpoints() {
		if c.RefType() == refType {
			return c, true
		}
	}
	return "", false
}

// ApprovedState is the Core State Machine state the Project moves to when the
// checkpoint is approved (spec/spec.md §22).
func (c Checkpoint) ApprovedState() ProjectState {
	switch c {
	case CheckpointScript, CheckpointCharacterDesign, CheckpointProductKeyVisual:
		return StateVisualDesign
	case CheckpointMainVisual, CheckpointSceneComplete:
		return StateGenerating
	case CheckpointRoughCut:
		return StateAudio
	}
	return StateReady // CheckpointFinal
}

// ChangesRequestedState is the state the Project returns to so the stage can be redone.
func (c Checkpoint) ChangesRequestedState() ProjectState {
	switch c {
	case CheckpointScript:
		return StatePlanning
	case CheckpointCharacterDesign, CheckpointMainVisual, CheckpointProductKeyVisual:
		return StateVisualDesign
	case CheckpointSceneComplete:
		return StateRepairing
	}
	return StateEditing // CheckpointRoughCut, CheckpointFinal
}

// EscalationResumeState is where the Project continues once an Escalation card is
// answered. The answer is recorded only: the user carries the decision out by hand (raising a
// limit, adopting a Take, fixing the Shot), and the Editor Judge or an edit brings the Project back to EDITING.
const EscalationResumeState = StateRepairing

// RequiresApproval reports whether an approval card is shown at cp for this level.
// custom is the user's selection and only matters for CUSTOM CHECK.
func (l ConfirmationLevel) RequiresApproval(cp Checkpoint, custom []Checkpoint) bool {
	switch l {
	case ConfirmKeyCheck:
		return slices.Contains(KeyCheckpoints(), cp)
	case ConfirmCustomCheck:
		return slices.Contains(custom, cp)
	}
	return false // FULL AUTO: only Escalation asks the user
}

// Valid reports whether l is a known level.
func (l ConfirmationLevel) Valid() bool {
	return l == ConfirmFullAuto || l == ConfirmKeyCheck || l == ConfirmCustomCheck
}
