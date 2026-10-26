// Package editorpanel draws the editing screen of a Project (Rough Cut, Editor Judge, Audio, Final QC, Export) and the
// answers of its stages. The export options and the voice form are inputs kept on the page: a stage answers with the
// regions that show its result (hx-swap-oob), never with the container that holds the inputs (#731).
package editorpanel

import (
	"fmt"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Data is the editing state of a Project (GET /projects/:id/edit): Rough Cut, Editor
// Judge, Audio, Final QC and Export (F-18 Simple, F-20 Simple, F-26).
type Data struct {
	Project domain.Project
	// BranchID is the Branch being edited ("" is the trunk): every Branch has its own Edit Timeline.
	BranchID string
	// PendingShots is how many additional Shots of the Editor → Reshoot Loop are still being made.
	PendingShots int
	// TimelineID is the latest Edit Timeline version ("" before the first Rough Cut).
	TimelineID string
	Clips      int
	Duration   float64
	Judge      *domain.EditorJudgeResult
	Audio      *domain.AudioTimelineVersion
	// TrunkAudio is the latest Audio Timeline of the trunk: Final QC inspects it, not the Branch shown.
	TrunkAudio *domain.AudioTimelineVersion
	// Voices are the cloned voices of the Project's speakers.
	Voices  []domain.VoiceClone
	QC      *domain.FinalQCResult
	Exports []domain.Export
	// Defaults are the export options preselected.
	Defaults domain.ExportOptions
	// CanRender is false without ffmpeg.
	CanRender bool
}

// View draws the answer to a stage action of the panel (ActionsFragment, VoiceCloned).
type View func(Data) templ.Component

// branchVals is the hx-vals that tell a stage action which Branch it works on.
func branchVals(branchID string) string {
	return atoms.HxVals(map[string]string{"branch_id": branchID})
}

// CanRequestShots reports whether the Director can be asked for the additional Shots the Editor
// Judge found missing.
func (d Data) CanRequestShots() bool {
	return d.Judge != nil && d.Judge.Verdict == domain.EditorNeedShot && d.Project.State == domain.StateEditJudging && d.PendingShots == 0
}

func needLabel(n *domain.ShotNeed) string {
	kind := "インサートショット"
	if n.Kind == domain.ClipReaction {
		kind = "リアクションショット"
	}
	return fmt.Sprintf("%s %.1f 秒（クリップ %d の後）", kind, n.Seconds(), n.AfterClip)
}

// CanRoughCut reports whether a Rough Cut can be made (or made again) now: the Project is in a state
// it can take an edit in (domain.ProjectState.CanEdit, the one the server refuses by with 409 and the
// editor's read-only flag follows).
func (d Data) CanRoughCut() bool { return d.Project.State.CanEdit() }

// CanJudge reports whether the Editor Judge can be run now.
func (d Data) CanJudge() bool {
	return d.TimelineID != "" && (d.Project.State == domain.StateEditing || d.Project.State == domain.StateEditJudging || d.Project.State == domain.StateRepairing)
}

// CanAudio reports whether the audio can be made now: the series shown has an Edit Timeline to make
// it for (the server refuses with 404 without one).
func (d Data) CanAudio() bool {
	return d.CanRender && d.Project.State == domain.StateAudio && d.TimelineID != ""
}

// CanFinalQC reports whether Final QC can be run now. It inspects the trunk whatever series is
// shown, so the audio it needs is the trunk's (TrunkAudio), not the Branch's.
func (d Data) CanFinalQC() bool {
	return d.TrunkAudio != nil && (d.Project.State == domain.StateAudio || d.Project.State == domain.StateFinalQC)
}

// FinalQCBlockedByTrunk is true when only the missing audio of the trunk keeps Final QC from running
// from a Branch's panel.
func (d Data) FinalQCBlockedByTrunk() bool {
	return d.BranchID != "" && d.TrunkAudio == nil && (d.Project.State == domain.StateAudio || d.Project.State == domain.StateFinalQC)
}

// CanExport reports whether the video can be exported now: the Project is in a state it can be
// exported from (domain.ProjectState.CanExport) and the series shown (the trunk or the Branch) has
// audio made for its latest Edit Timeline (domain.AudioTimelineVersion.CurrentFor): the server
// refuses an export with 404 / 409 otherwise.
func (d Data) CanExport() bool {
	return d.CanRender && d.Project.State.CanExport() && d.Audio.CurrentFor(d.TimelineID)
}

// ExportBlocker is the reason the export button is disabled although the Project can be exported
// from its state ("" when the button is enabled, or the state or ffmpeg is what disables it).
func (d Data) ExportBlocker() string {
	switch {
	case !d.CanRender || !d.Project.State.CanExport() || d.CanExport():
		return ""
	case d.Audio == nil:
		return "この系列にはまだ音声がないため、書き出せません。"
	}
	return "この系列の音声が最新の編集に対応していないため、書き出せません。"
}

func verdictLabel(v domain.EditorVerdict) string {
	switch v {
	case domain.EditorPass:
		return "PASS"
	case domain.EditorReEdit:
		return "Re-edit"
	}
	return "素材不足"
}

func verdictTone(pass bool) atoms.BadgeTone {
	if pass {
		return atoms.BadgeOK
	}
	return atoms.BadgeWarn
}

func clipPosition(index int) string {
	if index <= 0 {
		return "全体"
	}
	return fmt.Sprintf("クリップ %d", index)
}

func exportURL(projectID, exportID string) string {
	return paths.Project(projectID, "exports", exportID, "video")
}

func qcLabel(v domain.FinalQCVerdict) string {
	if v == domain.FinalQCPass {
		return "PASS"
	}
	return "FAIL"
}
