package gentest

import "github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"

// The media of a result (ResultView.Media), the status of a result (ResultView.Status) and the role of an input image.
const (
	MediaImage      = gentestrepo.MediaImage
	MediaVideo      = gentestrepo.MediaVideo
	StatusQueued    = gentestrepo.StatusQueued
	StatusRunning   = gentestrepo.StatusRunning
	StatusSucceeded = gentestrepo.StatusSucceeded
	StatusFailed    = gentestrepo.StatusFailed
	StatusCancelled = gentestrepo.StatusCancelled
	RoleReference   = gentestrepo.RoleReference
	RoleFirstFrame  = gentestrepo.RoleFirstFrame
	RoleLastFrame   = gentestrepo.RoleLastFrame
	RoleFace        = gentestrepo.RoleFace
	RoleStyle       = gentestrepo.RoleStyle
	RoleEdit        = gentestrepo.RoleEdit
	RoleMask        = gentestrepo.RoleMask
	RolePose        = gentestrepo.RolePose
	RoleMarkup      = gentestrepo.RoleMarkup
)
