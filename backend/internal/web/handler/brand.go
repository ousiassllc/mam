package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

func brandListData(v service.BrandView) organisms.BrandConstraintListData {
	return organisms.BrandConstraintListData{ProjectID: v.Project.ID, Constraints: v.Constraints}
}

// AddBrandConstraint registers a Brand / Safety Constraint (POST /projects/:id/brand-constraints).
// Invalid input re-renders the list with the user's input and the error (422).
func AddBrandConstraint(svc *service.BrandService) gin.HandlerFunc {
	return func(c *gin.Context) {
		form := service.BrandForm{Type: c.PostForm("type"), Level: c.PostForm("level"), Text: c.PostForm("text")}
		view, err := svc.Add(c.Request.Context(), c.Param("id"), form)
		if err == nil {
			render(c, http.StatusOK, organisms.BrandConstraintList(brandListData(view)))
			return
		}
		if invalid := asValidation(err); invalid != nil {
			current, lerr := svc.List(c.Request.Context(), c.Param("id"))
			if lerr != nil {
				toast(c, lerr)
				return
			}
			data := brandListData(current)
			data.Typed = map[string]string{"type": form.Type, "level": form.Level, "text": form.Text}
			data.Errors = map[string]string{invalid.Field: invalid.Message}
			render(c, http.StatusUnprocessableEntity, organisms.BrandConstraintList(data))
			return
		}
		toast(c, err)
	}
}

// DeleteBrandConstraint removes a constraint (DELETE /brand-constraints/:id).
func DeleteBrandConstraint(svc *service.BrandService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Delete(c.Request.Context(), c.Param("id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.BrandConstraintList(brandListData(view)))
	}
}

func brandTemplatePanelData(v service.TemplatesView) organisms.BrandTemplatePanelData {
	return organisms.BrandTemplatePanelData{ProjectID: v.ProjectID, Templates: v.Templates}
}

// SaveBrandTemplate saves the Project's constraints as a Brand Template
// (POST /projects/:id/brand-templates). Invalid input re-renders the panel with the input and the
// error (422).
func SaveBrandTemplate(svc *service.BrandService) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.PostForm("name")
		view, err := svc.SaveTemplate(c.Request.Context(), c.Param("id"), name)
		if err == nil {
			render(c, http.StatusOK, organisms.BrandTemplatePanel(brandTemplatePanelData(view)))
			return
		}
		if invalid := asValidation(err); invalid != nil {
			current, lerr := svc.TemplatesFor(c.Request.Context(), c.Param("id"))
			if lerr != nil {
				toast(c, lerr)
				return
			}
			data := brandTemplatePanelData(current)
			data.Typed = map[string]string{"name": name}
			data.Errors = map[string]string{invalid.Field: invalid.Message}
			render(c, http.StatusUnprocessableEntity, organisms.BrandTemplatePanel(data))
			return
		}
		toast(c, err)
	}
}

// ApplyBrandTemplate copies a Brand Template's constraints into the Project
// (POST /projects/:id/brand-templates/:templateId/apply) and answers with the Project's constraints.
func ApplyBrandTemplate(svc *service.BrandService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.ApplyTemplate(c.Request.Context(), c.Param("id"), c.Param("templateId"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.BrandConstraintList(brandListData(view)))
	}
}

// DeleteBrandTemplate removes a Brand Template (DELETE /projects/:id/brand-templates/:templateId);
// the Projects that used it keep their constraints.
func DeleteBrandTemplate(svc *service.BrandService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.DeleteTemplate(c.Request.Context(), c.Param("id"), c.Param("templateId"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.BrandTemplatePanel(brandTemplatePanelData(view)))
	}
}
