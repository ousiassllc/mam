package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// WorldStatePage renders the current World State (GET /projects/:id/world-state): the full page,
// or the view fragment for HTMX. Branches are M2, so a branch_id addresses nothing yet (404).
func WorldStatePage(svc *service.WorldService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Query("branch_id") != "" {
			pageError(c, service.ErrNotFound)
			return
		}
		view, err := svc.View(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		data := worldStateData(view)
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.WorldStateView(data))
			return
		}
		render(c, http.StatusOK, pages.WorldStatePage(middleware.CSRFToken(c), data))
	}
}

// UpdateCharacter sets the attributes named in the form (PUT /projects/:id/characters/:characterId)
// and answers with the updated view. A LOCKed attribute is refused with 423 and nothing is written.
// UpdateProduct and UpdateLocation do the same for Products and Locations.
func UpdateCharacter(svc *service.WorldService) gin.HandlerFunc {
	return updateEntity(svc, domain.TargetCharacter, "characterId")
}

// UpdateProduct is PUT /projects/:id/products/:productId.
func UpdateProduct(svc *service.WorldService) gin.HandlerFunc {
	return updateEntity(svc, domain.TargetProduct, "productId")
}

// UpdateLocation is PUT /projects/:id/locations/:locationId.
func UpdateLocation(svc *service.WorldService) gin.HandlerFunc {
	return updateEntity(svc, domain.TargetLocation, "locationId")
}

func updateEntity(svc *service.WorldService, target domain.WorldTarget, param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Query("branch_id") != "" {
			toast(c, service.ErrNotFound)
			return
		}
		fields := map[string]string{}
		for _, name := range domain.EditableFields(target) {
			if v, ok := c.GetPostForm(name); ok {
				fields[name] = strings.TrimSpace(v)
			}
		}
		view, err := svc.UpdateEntity(c.Request.Context(), c.Param("id"), target, c.Param(param), fields)
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.WorldStateView(worldStateData(view)))
	}
}

// SetWorldLock switches an attribute of the World State between AUTO and LOCK
// (PUT /projects/:id/world-state/locks) and answers with the updated view.
func SetWorldLock(svc *service.WorldService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.SetFieldLock(c.Request.Context(), c.Param("id"), domain.WorldTarget(c.PostForm("target_type")),
			c.PostForm("target_id"), c.PostForm("parameter"), domain.LockMode(c.PostForm("mode")))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.WorldStateView(worldStateData(view)))
	}
}

// AssignCharacterReference is PUT /projects/:id/characters/:characterId/reference: a Library image
// on the face or body slot. AssignProductReference and AssignLocationReference do the same for
// Products (geometry, label) and Locations (layout).
func AssignCharacterReference(svc *service.WorldService) gin.HandlerFunc {
	return assignReference(svc, domain.TargetCharacter, "characterId")
}

// AssignProductReference is PUT /projects/:id/products/:productId/reference.
func AssignProductReference(svc *service.WorldService) gin.HandlerFunc {
	return assignReference(svc, domain.TargetProduct, "productId")
}

// AssignLocationReference is PUT /projects/:id/locations/:locationId/reference.
func AssignLocationReference(svc *service.WorldService) gin.HandlerFunc {
	return assignReference(svc, domain.TargetLocation, "locationId")
}

func assignReference(svc *service.WorldService, target domain.WorldTarget, param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.AssignReference(c.Request.Context(), c.Param("id"), target, c.Param(param), c.PostForm("slot"), c.PostForm("reference_id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.WorldStateView(worldStateData(view)))
	}
}

func worldStateData(v service.WorldView) organisms.WorldStateData {
	return organisms.WorldStateData{Project: v.Project, World: v.World, Locks: v.Locks, ProjectLocks: v.ProjectLocks, Library: v.Library}
}
