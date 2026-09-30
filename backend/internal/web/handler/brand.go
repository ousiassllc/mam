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
