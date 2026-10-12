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
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func seriesData(v service.SeriesView) organisms.SeriesViewData {
	return organisms.SeriesViewData{Series: v.Series, Canon: v.Canon, Versions: v.Versions, Episodes: v.Episodes, Templates: v.Templates}
}

// SeriesPage renders a Series (GET /series/:id): the full page, or the view fragment for HTMX.
func SeriesPage(svc *service.SeriesService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.View(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.SeriesView(seriesData(view)))
			return
		}
		render(c, http.StatusOK, pages.SeriesPage(middleware.CSRFToken(c), seriesData(view)))
	}
}

// CreateSeries makes a Series (POST /series, form fields name and from_project_id) and redirects
// to its page (HX-Redirect, 201).
func CreateSeries(svc *service.SeriesService) gin.HandlerFunc {
	return func(c *gin.Context) {
		series, err := svc.Create(c.Request.Context(), c.PostForm("name"), c.PostForm("from_project_id"))
		if err != nil {
			toast(c, err)
			return
		}
		c.Header("HX-Redirect", paths.Series(series.ID))
		c.Status(http.StatusCreated)
	}
}

// CreateEpisode starts the next Episode of a Series (POST /series/:id/episodes, form field
// previous_episode_id and optional brand_template_id) and redirects to the new Project (HX-Redirect, 201).
func CreateEpisode(svc *service.SeriesService) gin.HandlerFunc {
	return func(c *gin.Context) {
		project, err := svc.CreateEpisode(c.Request.Context(), c.Param("id"), c.PostForm("previous_episode_id"), c.PostForm("brand_template_id"))
		if err != nil {
			toast(c, err)
			return
		}
		c.Header("HX-Redirect", paths.Project(project.ID))
		c.Status(http.StatusCreated)
	}
}

// UpdateSeriesCharacter sets the canonical attributes named in the form
// (PUT /series/:id/characters/:characterId) and answers with the form of that Character, with the new
// version carried along (the other forms are not part of it). Existing Episodes are not changed.
func UpdateSeriesCharacter(svc *service.SeriesService) gin.HandlerFunc {
	return func(c *gin.Context) {
		fields := map[string]string{}
		for _, name := range domain.SeriesCharacterFields() {
			if v, ok := c.GetPostForm(name); ok {
				fields[name] = strings.TrimSpace(v)
			}
		}
		view, err := svc.UpdateCharacter(c.Request.Context(), c.Param("id"), c.Param("characterId"), fields)
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.SeriesCharacterSaved(seriesData(view), c.Param("characterId")))
	}
}

// UpdateSeriesStyle sets the canonical Style (PUT /series/:id/style, form field style) and answers
// with the Style form, with the new version carried along.
func UpdateSeriesStyle(svc *service.SeriesService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.UpdateStyle(c.Request.Context(), c.Param("id"), c.PostForm("style"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.SeriesStyleSaved(seriesData(view)))
	}
}
