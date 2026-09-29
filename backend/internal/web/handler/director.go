package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// SetMode switches AUTO ⇄ DIRECTOR (PUT /projects/:id/mode) and answers with the refreshed control.
// The thread has a new AI message, so the client is told to fetch it.
func SetMode(svc *service.DirectorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		project, err := svc.SetMode(c.Request.Context(), c.Param("id"), domain.Mode(c.PostForm("mode")))
		if err != nil {
			toast(c, err)
			return
		}
		c.Header("HX-Trigger", refreshTrigger)
		render(c, http.StatusOK, molecules.ModeControl(project))
	}
}

// parameterOf reads the parameter of a lock request; target_type is the name the Project locks use
// in spec/api/endpoints.md §2.1.
func parameterOf(c *gin.Context) domain.LockParameter {
	if p := c.PostForm("parameter"); p != "" {
		return domain.LockParameter(p)
	}
	return domain.LockParameter(c.PostForm("target_type"))
}

// SetProjectLock sets Story or Music (PUT /projects/:id/locks) and answers with the switch.
func SetProjectLock(svc *service.DirectorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := parameterOf(c)
		locks, err := svc.SetProjectLock(c.Request.Context(), c.Param("id"), p, domain.LockMode(c.PostForm("mode")))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, atoms.LockToggle(organisms.ProjectLockToggle(c.Param("id"), p, locks)))
	}
}

// SetShotLock sets one parameter of a Shot (PUT /shots/:id/locks) and answers with the switch. A
// LOCKed value that would be replaced is refused with 423.
func SetShotLock(svc *service.DirectorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := parameterOf(c)
		locks, err := svc.SetShotLock(c.Request.Context(), c.Param("id"), p, domain.LockMode(c.PostForm("mode")), c.PostForm("value"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, atoms.LockToggle(organisms.LockToggleFor(c.Param("id"), p, locks)))
	}
}

func inspectorData(v service.Inspector) organisms.ShotInspectorData {
	return organisms.ShotInspectorData{
		Project: v.Project, Scene: v.Scene, Shot: v.Shot, Spec: v.Spec, Takes: v.Takes, Locks: v.Locks,
		SpecText: v.SpecText(), SpecError: v.SpecError,
	}
}

// ShotInspector renders the Shot Inspector (GET /shots/:id): the full page, or the inspector for HTMX.
func ShotInspector(svc *service.DirectorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Inspector(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.ShotInspector(inspectorData(view)))
			return
		}
		render(c, http.StatusOK, pages.ShotInspectorPage(middleware.CSRFToken(c), inspectorData(view)))
	}
}

// EditShotSpec stores a hand-edited Canonical Shot Spec (PUT /shots/:id/shot-spec) and answers with
// the inspector. An unusable spec is 422 and the inspector is drawn again with the input and the
// reason; a change to a LOCKed parameter is 423.
func EditShotSpec(svc *service.DirectorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.EditSpec(c.Request.Context(), c.Param("id"), c.PostForm("shot_spec"))
		if err != nil {
			if v := asValidation(err); v != nil && view.Shot.ID != "" {
				render(c, http.StatusUnprocessableEntity, organisms.ShotInspector(inspectorData(view)))
				return
			}
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.ShotInspector(inspectorData(view)))
	}
}

// DirectorCommand carries out a natural language direction for the Shot (POST /shots/:id/director-command).
// It answers 202 with the inspector when a regeneration was queued, 200 otherwise, and 423 when
// the direction changes a LOCKed parameter.
func DirectorCommand(svc *service.DirectorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		res, err := svc.DirectorCommand(c.Request.Context(), c.Param("id"), c.PostForm("content"))
		if err != nil {
			toast(c, err)
			return
		}
		status := http.StatusOK
		if res.Started {
			status = http.StatusAccepted
		}
		c.Header("HX-Trigger", refreshTrigger)
		render(c, status, organisms.ShotInspector(inspectorData(res.Inspector)))
	}
}
