// Package filestate judges the files of a catalog model on disk: whether every required file is in place, which optional ones (a speed mode's LoRA) are not,
// and whether the files are all there without an install record. The model manager's catalog view is its only judge, and the generation test's "installed"
// is the catalog's answer (mmview.InfoOf), so the two lists cannot disagree (#1025).
package filestate

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// Report is what Add collected and Seal decided. Its exported fields are the ones of the model view (an embedded struct: the JSON keys are the view's own).
type Report struct {
	// MissingOptional are the optional files that are not in place: the model is installed without them, and what needs them (a speed mode) does not work.
	MissingOptional []string `json:"missing_optional_files,omitempty"`
	// PartialNote says so in a sentence, for an installed model that lacks optional files ("" when none is missing).
	PartialNote string `json:"partial_note,omitempty"`
	// FilesInPlace: every required file is on disk but no install is recorded (state.json was lost, or an install broke off): installing again records it and
	// keeps the verified files.
	FilesInPlace bool `json:"files_in_place,omitempty"`

	required, requiredPresent int
}

// Add counts a file of the model and whether it is in place.
func (r *Report) Add(f catalog.File, present bool) {
	switch {
	case !f.Optional:
		r.required++
		if present {
			r.requiredPresent++
		}
	case !present:
		r.MissingOptional = append(r.MissingOptional, f.Name)
	}
}

// RequiredInPlace reports whether every required file is in place: the one condition of "installed" (with the install record).
func (r *Report) RequiredInPlace() bool { return r.requiredPresent == r.required }

// Seal fills the exported fields once the state is known: an installed model (the record is there and the required files are) gets its note; a model that
// is not recorded gets FilesInPlace when its required files are all there; the missing optional files are only said of an installed model.
func (r *Report) Seal(installed, recorded bool) {
	switch {
	case installed:
		r.PartialNote = partialNote(r.MissingOptional)
	case !recorded:
		r.FilesInPlace = r.RequiredInPlace()
		fallthrough
	default:
		r.MissingOptional = nil
	}
}

// partialNote says in a sentence that an installed model lacks optional files: what works without them, what does not, and how to get them.
func partialNote(missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	return "導入済みです（速さのモードなど、任意のファイルを使う機能の一部は未導入）。必須のファイルは揃っているので生成できますが、次の任意のファイルが無いため、それを使う機能は使えません: " +
		strings.Join(missing, "、") + "。モデルマネージャーの「任意ファイルを取得」で入れられます"
}
