// Package shotinspector draws the Shot Inspector (F-05, F-06) and the answers of its forms: each form answers with its own
// section, so that what is typed in the others is kept.
package shotinspector

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

// Data is the state of the Shot Inspector (GET /shots/:id).
type Data struct {
	Project domain.Project
	Scene   domain.Scene
	Shot    domain.Shot
	Spec    *domain.ShotSpec // nil until a spec exists
	Takes   []domain.Take
	Locks   domain.ParameterLocks
	// Pack is the Reference Pack of the Shot; Library the Reference Library images that can be added to it.
	Pack    []domain.Reference
	Library []domain.ReferenceItem
	// SpecText is the spec as the edit form shows it; SpecError is why a manual edit was rejected.
	SpecText  string
	SpecError string
	// Versions are the saved versions of the spec, newest first (F-28).
	Versions []domain.VersionEntry
	// Judged is the latest Judge result of each Take that was judged, by Take ID.
	Judged map[string]domain.JudgeResult
}

// restoreBlocked is why the Shot's spec cannot be restored from a version now ("" when it can). The
// Inspector shows the state of the moment it was loaded: the actions the server refuses in that state
// (domain.ShotState.CanEditSpec / CanPinReference, the predicates the repositories use) are not drawn.
func (d Data) restoreBlocked() string {
	if d.Shot.State.CanEditSpec() {
		return ""
	}
	return "生成・Repair 中は復元できません。終わってから開き直してください。"
}

// generationModel is the model the next Take is made with: the one LOCKed or set MANUAL for the Shot (not AUTO), otherwise
// the one of its latest Take, otherwise the Orchestrator's choice.
func (d Data) generationModel() string {
	if m := d.Locks.Text(domain.ParamVideoModel); m != "" && d.Locks.Mode(domain.ParamVideoModel) != domain.LockAuto {
		return m
	}
	if n := len(d.Takes); n > 0 && d.Takes[n-1].ModelName != "" {
		return d.Takes[n-1].ModelName
	}
	return "自動選択（生成時に決定）"
}

// judgeSummary is the one line of a Take's latest Judge result: the verdict and confidence.
func judgeSummary(r domain.JudgeResult) (atoms.BadgeTone, string) {
	return organisms.JudgeTone(r.Result), fmt.Sprintf("Judge: %s（確信度 %.0f%%）", r.Result.Label(), r.OverallConfidence*100)
}

// issueLine is one Issue of a Judge result as the Take List shows it.
func issueLine(is domain.JudgeIssue) string {
	var b strings.Builder
	if is.LockViolation {
		b.WriteString("LOCK 違反 / ")
	}
	b.WriteString(string(is.Severity) + " / " + is.Type)
	if is.TimeRange != "" {
		b.WriteString(" / " + is.TimeRange)
	}
	if is.Description != "" {
		b.WriteString(": " + is.Description)
	}
	return b.String()
}

// specRows lists the parts of the Canonical Shot Spec as label / value rows.
func specRows(s domain.ShotSpec) [][2]string {
	rows := [][2]string{{"Objective", s.ShotObjective}, {"Duration", s.Duration}}
	if c := s.Character; c != nil {
		rows = append(rows, [2]string{"Character", joinNonEmpty(c.ID, "identity: "+c.Identity, "wardrobe: "+c.Wardrobe, "hair: "+c.Hair)})
	}
	if p := s.Product; p != nil {
		rows = append(rows, [2]string{"Product", joinNonEmpty(p.ID, "geometry: "+p.Geometry, "label: "+p.Label, "orientation: "+p.Orientation)})
	}
	if l := s.Location; l != nil {
		rows = append(rows, [2]string{"Location", joinNonEmpty(l.ID, l.Time, l.Weather)})
	}
	for _, b := range s.Acting {
		rows = append(rows, [2]string{"Acting " + b.Time, b.Action})
	}
	rows = append(rows,
		[2]string{"Camera", joinNonEmpty(s.Camera.Lens, s.Camera.Framing, s.Camera.Movement)},
		[2]string{"Lighting", joinNonEmpty(s.Lighting.Inherit, s.Lighting.KeyDirection, s.Lighting.Mood)},
		[2]string{"Continuity", fmt.Sprintf("start_from_previous_final_state: %t", s.Continuity.StartFromPreviousFinalState)},
	)
	if len(s.Constraints) > 0 {
		rows = append(rows, [2]string{"Constraints", strings.Join(s.Constraints, ", ")})
	}
	return rows
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if !strings.HasSuffix(p, ": ") && strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " / ")
}
