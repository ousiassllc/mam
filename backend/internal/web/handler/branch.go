package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// BranchTreePage renders the Branch page without a selection (GET /projects/:id/branches).
func BranchTreePage(svc *service.BranchService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tree, err := svc.Tree(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		data := branchPageData(tree, nil)
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.BranchView(data))
			return
		}
		render(c, http.StatusOK, pages.BranchPage(middleware.CSRFToken(c), data))
	}
}

// BranchPage renders one Branch (GET /branches/:id): the detail fragment for HTMX (what
// mam-branch-tree swaps into #branch-detail on branch-selected), or the Branch page with that
// Branch selected.
func BranchPage(svc *service.BranchService) gin.HandlerFunc {
	return func(c *gin.Context) {
		detail, err := svc.Detail(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		selected := branchDetailData(detail)
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.BranchDetail(selected))
			return
		}
		tree, err := svc.Tree(c.Request.Context(), detail.Project.ID)
		if err != nil {
			pageError(c, err)
			return
		}
		render(c, http.StatusOK, pages.BranchPage(middleware.CSRFToken(c), branchPageData(tree, &selected)))
	}
}

// CreateBranch saves a Branch Point at the Shot (POST /shots/:id/branch) and sends the browser to
// the page of the new Branch.
func CreateBranch(svc *service.BranchService) gin.HandlerFunc {
	return func(c *gin.Context) {
		detail, err := svc.Create(c.Request.Context(), c.Param("id"), service.BranchInput{
			PointType:     domain.BranchPointType(c.PostForm("point_type")),
			OffsetSeconds: c.PostForm("offset_seconds"),
			Name:          c.PostForm("name"),
			Direction:     c.PostForm("direction"),
		})
		if err != nil {
			toast(c, err)
			return
		}
		c.Header("HX-Redirect", "/branches/"+detail.Branch.ID)
		c.Status(http.StatusCreated)
	}
}

// UpdateBranch renames the Branch or moves its World State reference (PUT /branches/:id) and answers
// with the detail fragment; the tree is told to reload.
func UpdateBranch(svc *service.BranchService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var name, version *string
		if v, ok := c.GetPostForm("name"); ok {
			name = &v
		}
		if v, ok := c.GetPostForm("world_state_version"); ok {
			version = &v
		}
		detail, err := svc.Update(c.Request.Context(), c.Param("id"), name, version)
		if err != nil {
			toast(c, err)
			return
		}
		c.Header("HX-Trigger", "branches-changed")
		render(c, http.StatusOK, organisms.BranchDetail(branchDetailData(detail)))
	}
}

func branchPageData(t service.BranchTree, selected *organisms.BranchDetailData) organisms.BranchPageData {
	shots := make([]organisms.BranchShotOption, len(t.Shots))
	for i, s := range t.Shots {
		shots[i] = organisms.BranchShotOption{ShotID: s.ShotID, Label: s.Label}
	}
	return organisms.BranchPageData{Project: t.Project, Shots: shots, Selected: selected}
}

func branchDetailData(d service.BranchDetail) organisms.BranchDetailData {
	return organisms.BranchDetailData{
		Project: d.Project, Branch: d.Branch, Parent: d.Parent, Scenes: d.Scenes, Shots: branchShotItems(d.Shots),
		Versions: d.Versions, LatestWorldVersion: d.LatestWorldVersion, Started: d.Started,
	}
}

func branchShotItems(refs []repository.ShotRef) []organisms.BranchShotItem {
	items := make([]organisms.BranchShotItem, len(refs))
	for i, r := range refs {
		items[i] = organisms.BranchShotItem{ShotID: r.Shot.ID, Scene: r.Scene.OrderIndex + 1, Shot: r.Shot.OrderIndex + 1, Objective: r.Shot.Objective}
	}
	return items
}
