// Package media is the "画像・動画モデル" category of the settings screen (spec/components/settings-screen-categories.md §3):
// a table of the image purposes and a table of the video Quality Policies, each row with its models, and the modal that
// edits one row. A save is one row's (it takes effect at once); the whole assignment is saved by PUT /settings/models (kept
// for the tests) and reset by DELETE /settings/models.
package media

import "slices"

// The two models of a route.
const (
	SlotPrimary  = "primary"
	SlotFallback = "fallback"
)

// ImageModelField is the name of the model <select> of an image purpose's slot.
func ImageModelField(purpose, slot string) string { return "image_" + purpose + "_" + slot + "_model" }

// ImageFastField is the name of the "fast variant" checkbox of an image purpose's slot.
func ImageFastField(purpose, slot string) string { return "image_" + purpose + "_" + slot + "_fast" }

// PolicyTargetField is the name of the <select> of a Quality Policy's slot: its options carry a model and
// a resolution together (see VideoChoice), so that only a resolution the model supports can be picked.
func PolicyTargetField(policy, slot string) string { return "policy_" + policy + "_" + slot }

// PolicyTakesField is the name of the number of Takes of a Quality Policy.
func PolicyTakesField(policy string) string { return "policy_" + policy + "_takes" }

// The ids the page, the answers and the script share.
const (
	// PanelID is what the answers about the whole assignment replace.
	PanelID = "model-assignment"
	// StatusID is the notes and the reset button that a save of one row may change (replaced out of band with the row).
	StatusID = "model-status"
	// ImagesID and PoliciesID are the lists of the rows.
	ImagesID   = "media-images"
	PoliciesID = "media-policies"
)

// ImageRowID is the id of the row of an image purpose: what a save replaces out of band.
func ImageRowID(purpose string) string { return "media-image-" + purpose }

// PolicyRowID is the id of the row of a Quality Policy.
func PolicyRowID(policy string) string { return "media-policy-" + policy }

// ImageFocusKey names the button that opens the modal of an image purpose (it stays the same whatever the row says).
func ImageFocusKey(purpose string) string { return "edit-image-" + purpose }

// PolicyFocusKey names the button that opens the modal of a Quality Policy.
func PolicyFocusKey(policy string) string { return "edit-policy-" + policy }

// ImageModel is an image model that may be chosen. Fast: it has a fast variant. AcceptsReferences:
// the Model Adapter hands it reference images.
type ImageModel struct {
	Name              string
	Fast              bool
	AcceptsReferences bool
}

// ImageTarget is the saved choice of one slot of an image purpose. Unusable: the model is not among the choices (an adult
// model while the age confirmation is off), so the slot cannot be used until another is chosen.
type ImageTarget struct {
	Model    string
	Fast     bool
	Unusable bool
}

// ImageRow is one image purpose. ReferenceNote, when set, says which of its models do not use reference images.
type ImageRow struct {
	Purpose       string
	Label         string
	Hint          string
	Primary       ImageTarget
	Fallback      ImageTarget
	ReferenceNote string
}

// VideoChoice is one option of a video slot: a model at a resolution it supports (Value is
// "<model>@<resolution>").
type VideoChoice struct {
	Value string
	Label string
}

// PolicyRow is one Quality Policy: the video models (a VideoChoice.Value each), whether each is unusable, and the number
// of Takes.
type PolicyRow struct {
	Policy           string
	Label            string
	Primary          string
	Fallback         string
	PrimaryUnusable  bool
	FallbackUnusable bool
	Takes            int
}

// Data is the category: the saved assignment (the app applies a saved change at once). Pinned: the file was fixed by a
// start-up option, so the tables are read-only and PinnedNote names that option. Notice is the confirmation of a save or a
// reset of the whole assignment; Error is why the last save of the whole assignment was refused. Problem means the settings
// file cannot be read: the category then says so and offers nothing. Customized: the file differs from the defaults (the
// reset is offered).
type Data struct {
	Pinned     bool
	PinnedNote string
	Customized bool
	Notice     string
	Error      string
	Problem    string
	// GatedNote, when set, says the saved assignment names adult models that are not offered while the age confirmation is off.
	GatedNote string
	Images    []ImageRow
	Policies  []PolicyRow
}

// ImageForm is what the modal of an image purpose is drawn from: the row (after a refusal, what was typed), the models that
// may be chosen, and why the last save was refused.
type ImageForm struct {
	Row    ImageRow
	Models []ImageModel
	Error  string
	// Blame is the slot (SlotPrimary, SlotFallback) the refusal is about, "" when it is about none.
	Blame string
}

// PolicyForm is what the modal of a Quality Policy is drawn from.
type PolicyForm struct {
	Row     PolicyRow
	Choices []VideoChoice
	Error   string
	// Blame is the slot (SlotPrimary, SlotFallback) or BlameTakes the refusal is about, "" when it is about none.
	Blame string
}

// BlameTakes is PolicyForm.Blame for a refusal about the number of Takes.
const BlameTakes = "takes"

// unusableSuffix marks the option of a saved model that is not among the choices (an adult model while the age confirmation
// is off), so that the select shows the saved assignment instead of silently showing another model.
const unusableSuffix = "（現在は使えません）"

// showFast reports whether a slot offers the "fast variant" checkbox: only when a model has a fast variant
// (models.<id>.fast_model; no built-in model has one) or the saved route already asks for it.
func showFast(t ImageTarget, models []ImageModel) bool {
	return t.Fast || slices.ContainsFunc(models, func(m ImageModel) bool { return m.Fast })
}
