package organisms

import (
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// LockToggleFor is the AUTO / MANUAL / LOCK switch of a parameter of the Shot.
func LockToggleFor(shotID string, p domain.LockParameter, locks domain.ParameterLocks) molecules.LockToggleData {
	d := molecules.LockToggleData{DOMID: "lock-" + shotID + "-" + string(p), URL: paths.Shot(shotID, "locks"), Parameter: p, Mode: locks.Mode(p)}
	if p == domain.ParamVideoModel {
		d.Model = videoModelField(shotID, locks.Text(p))
	}
	return d
}

// ProjectLockToggle is the AUTO / MANUAL / LOCK switch of Story or Music.
func ProjectLockToggle(projectID string, p domain.LockParameter, locks domain.ParameterLocks) molecules.LockToggleData {
	return molecules.LockToggleData{DOMID: "lock-" + projectID + "-" + string(p), URL: paths.Project(projectID, "locks"), Parameter: p, Mode: locks.Mode(p)}
}

// videoSourceKey is the picker of a Shot's video model (the source "shot-video" of handler/modelpicker).
const videoSourceKey = "shot-video"

// videoModelField is the field that chooses the Video Model a Shot keeps with MANUAL and LOCK: the model picker, which shows the model chosen as its card does ("" is
// the invitation to choose: the current model is used). The value is posted as "value" with the mode button, as the pull-down it replaces did.
func videoModelField(shotID, model string) modelpick.FieldData {
	card := modelcard.Card{}
	if model != "" {
		answer := commercial.Of(model)
		card = modelcard.Card{ID: "lock-model-" + shotID, Name: modelreg.Label(model), Icon: atoms.IconVideo, Commercial: &answer}
	}
	return modelpick.FieldData{
		ID: "lock-model-" + shotID, Label: "Video Model", Current: card, URL: paths.ModelPickerSource(videoSourceKey),
		Inputs: []modelpick.Input{{Name: "value", Key: "value", Value: model}},
	}
}
