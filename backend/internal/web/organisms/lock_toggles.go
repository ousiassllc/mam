package organisms

import (
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// LockToggleFor is the AUTO / MANUAL / LOCK switch of a parameter of the Shot.
func LockToggleFor(shotID string, p domain.LockParameter, locks domain.ParameterLocks) molecules.LockToggleData {
	d := molecules.LockToggleData{DOMID: "lock-" + shotID + "-" + string(p), URL: paths.Shot(shotID, "locks"), Parameter: p, Mode: locks.Mode(p)}
	if p == domain.ParamVideoModel {
		d.Models, d.Model = domain.VideoModels(), locks.Text(p)
	}
	return d
}

// ProjectLockToggle is the AUTO / MANUAL / LOCK switch of Story or Music.
func ProjectLockToggle(projectID string, p domain.LockParameter, locks domain.ParameterLocks) molecules.LockToggleData {
	return molecules.LockToggleData{DOMID: "lock-" + projectID + "-" + string(p), URL: paths.Project(projectID, "locks"), Parameter: p, Mode: locks.Mode(p)}
}
