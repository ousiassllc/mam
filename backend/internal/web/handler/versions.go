package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/worldstate"
)

// RestoreShotVersion restores the spec of a Shot to a saved version
// (POST /shots/:id/versions/:versionNumber/restore) and answers with the Shot Inspector.
func RestoreShotVersion(svc *service.VersionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		number, err := service.ParseVersionNumber(c.Param("versionNumber"))
		if err != nil {
			toast(c, err)
			return
		}
		view, err := svc.RestoreShot(c.Request.Context(), c.Param("id"), number)
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.ShotInspector(inspectorData(view)))
	}
}

// RestoreWorldStateVersion restores the trunk World State, or that of the Branch named by the query
// branch_id, to a saved version (POST /projects/:id/world-state/versions/:versionNumber/restore)
// and answers with the view of the same scope.
func RestoreWorldStateVersion(svc *service.VersionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		number, err := service.ParseVersionNumber(c.Param("versionNumber"))
		if err != nil {
			toast(c, err)
			return
		}
		view, err := svc.RestoreWorldState(c.Request.Context(), c.Param("id"), c.Query("branch_id"), number)
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, worldstate.View(worldStateData(view)))
	}
}

// branchReloadTrigger is the HX-Trigger event that makes the Branch detail on the page load again
// (organisms.BranchDetail listens for it).
const branchReloadTrigger = "branch-reload"

func branchHistoryData(h service.BranchHistory) organisms.BranchHistoryData {
	return organisms.BranchHistoryData{BranchID: h.BranchID, Versions: h.Versions, World: h.World}
}

// BranchVersions shows the version history of a Branch (GET /branches/:id/versions).
func BranchVersions(svc *service.VersionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		h, err := svc.Branch(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		render(c, http.StatusOK, organisms.BranchVersionHistory(branchHistoryData(h)))
	}
}

// RestoreBranchVersion restores a Branch (name and World State reference) to a saved version
// (POST /branches/:id/versions/:versionNumber/restore) and answers with its history.
func RestoreBranchVersion(svc *service.VersionService) gin.HandlerFunc {
	return restoreBranch(svc.RestoreBranch)
}

// RestoreBranchWorldStateVersion restores the World State series of a Branch to a saved version
// (POST /branches/:id/world-state/versions/:versionNumber/restore) and answers with its history.
func RestoreBranchWorldStateVersion(svc *service.VersionService) gin.HandlerFunc {
	return restoreBranch(svc.RestoreBranchWorldState)
}

type branchRestore func(ctx context.Context, branchID string, number int) (service.BranchHistory, error)

func restoreBranch(restore branchRestore) gin.HandlerFunc {
	return func(c *gin.Context) {
		number, err := service.ParseVersionNumber(c.Param("versionNumber"))
		if err != nil {
			toast(c, err)
			return
		}
		h, err := restore(c.Request.Context(), c.Param("id"), number)
		if err != nil {
			toast(c, err)
			return
		}
		// The Branch page's detail (name, World State reference, versions) and tree show what was
		// restored; they reload on this event.
		c.Header("HX-Trigger", branchReloadTrigger)
		render(c, http.StatusOK, organisms.BranchVersionHistory(branchHistoryData(h)))
	}
}

// TakeTrace shows the Generation Traceability of a Take (GET /takes/:id/trace).
func TakeTrace(svc *service.TraceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tr, err := svc.Take(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		render(c, http.StatusOK, organisms.TakeTracePanel(tr))
	}
}
