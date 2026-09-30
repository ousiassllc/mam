package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

func policyFormData(v service.PolicyView, invalid *service.ValidationError, typed map[string]string, saved bool) organisms.PolicyFormData {
	d := organisms.PolicyFormData{Project: v.Project, Policy: v.Policy, Effective: v.Effective, Typed: typed, Saved: saved}
	if invalid != nil {
		d.Errors = map[string]string{invalid.Field: invalid.Message}
	}
	return d
}

func storagePanelData(v service.StorageView) organisms.StoragePanelData {
	return organisms.StoragePanelData{ProjectID: v.ProjectID, Storage: v.StorageSummary}
}

// SettingsPage is the Production Template, Quality/Cost Policy, cost and disk usage of a Project
// (GET /projects/:id/settings). HTMX requests get the policy forms without the page frame.
func SettingsPage(svc *service.PolicyService, brands *service.BrandService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Settings(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		brand, err := brands.List(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		templates, err := brands.TemplatesFor(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		data := pages.SettingsPageData{
			Policy: policyFormData(view.PolicyView, nil, nil, false), Cost: view.Cost, Storage: storagePanelData(view.Storage),
			Brand: brandListData(brand), Templates: brandTemplatePanelData(templates),
		}
		if isHTMX(c) {
			render(c, http.StatusOK, pages.SettingsBody(data))
			return
		}
		render(c, http.StatusOK, pages.SettingsPage(middleware.CSRFToken(c), data))
	}
}

// SetProductionTemplate switches the Production Template (PUT /projects/:id/production-template).
func SetProductionTemplate(svc *service.PolicyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.SetProductionTemplate(c.Request.Context(), c.Param("id"), domain.ProductionTemplate(c.PostForm("production_template")))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.ProductionTemplateForm(policyFormData(view, nil, nil, true)))
	}
}

// SetQualityPolicy sets the Quality Policy (PUT /projects/:id/quality-policy). A request that only
// carries quality_policy (the switch in the composer) leaves Max retries and the thresholds alone;
// the settings form carries them too, and an empty field there resets it to the default.
func SetQualityPolicy(svc *service.PolicyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var form *service.PolicyForm
		typed := map[string]string{}
		if _, ok := c.GetPostForm("max_retries"); ok {
			form = &service.PolicyForm{
				MaxRetries: c.PostForm("max_retries"), MaxGenerationSeconds: c.PostForm("max_generation_seconds"),
				MinAcceptanceScore: c.PostForm("min_acceptance_score"), MinAxisScore: c.PostForm("min_axis_score"),
			}
			typed = map[string]string{
				"max_retries": form.MaxRetries, "max_generation_seconds": form.MaxGenerationSeconds,
				"min_acceptance_score": form.MinAcceptanceScore, "min_axis_score": form.MinAxisScore,
			}
		}
		view, err := svc.SetQualityPolicy(c.Request.Context(), c.Param("id"), domain.QualityPolicy(c.PostForm("quality_policy")), form)
		if err == nil {
			if form == nil {
				c.Header("HX-Reswap", "none") // the composer's select already shows the choice
				c.Status(http.StatusNoContent)
				return
			}
			render(c, http.StatusOK, organisms.PolicyForm(policyFormData(view, nil, nil, true)))
			return
		}
		if invalid := asValidation(err); invalid != nil && form != nil {
			current, perr := svc.Policy(c.Request.Context(), c.Param("id"))
			if perr != nil {
				toast(c, perr)
				return
			}
			current.Policy.Quality = domain.QualityPolicy(c.PostForm("quality_policy"))
			render(c, http.StatusUnprocessableEntity, organisms.PolicyForm(policyFormData(current, invalid, typed, false)))
			return
		}
		toast(c, err)
	}
}

// SetBudget sets the budget ceilings (PUT /projects/:id/budget). Invalid input re-renders the form
// with the user's input and the error (422).
func SetBudget(svc *service.PolicyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		form := service.BudgetForm{Project: c.PostForm("project_budget"), Shot: c.PostForm("shot_budget")}
		view, err := svc.SetBudget(c.Request.Context(), c.Param("id"), form)
		if err == nil {
			render(c, http.StatusOK, organisms.BudgetForm(policyFormData(view, nil, nil, true)))
			return
		}
		if invalid := asValidation(err); invalid != nil {
			current, perr := svc.Policy(c.Request.Context(), c.Param("id"))
			if perr != nil {
				toast(c, perr)
				return
			}
			typed := map[string]string{"project_budget": form.Project, "shot_budget": form.Shot}
			render(c, http.StatusUnprocessableEntity, organisms.BudgetForm(policyFormData(current, invalid, typed, false)))
			return
		}
		toast(c, err)
	}
}

// CostSummary is the cost of the Project by Project, Scene and Shot (GET /projects/:id/cost-summary).
func CostSummary(svc *service.PolicyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		sum, err := svc.CostSummary(c.Request.Context(), c.Param("id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.CostSummaryPanel(c.Param("id"), sum))
	}
}

// ArchiveTake frees the video file of one Take (POST /projects/:id/takes/:takeId/archive).
func ArchiveTake(svc *service.PolicyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.ArchiveTake(c.Request.Context(), c.Param("id"), c.Param("takeId"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.StoragePanel(storagePanelData(view)))
	}
}

// ArchiveUnneededTakes frees the video files of every unneeded Take
// (POST /projects/:id/storage/archive-unneeded).
func ArchiveUnneededTakes(svc *service.PolicyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.ArchiveUnneeded(c.Request.Context(), c.Param("id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.StoragePanel(storagePanelData(view)))
	}
}
