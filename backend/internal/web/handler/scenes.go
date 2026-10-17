package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/sceneshot"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// ScenePage renders the Scene / Shot / Take hierarchy (GET /projects/:id/scenes/:sceneId, and
// GET /projects/:id/scenes, which selects the first Scene): the full page, or the view fragment
// for HTMX. The query `shot` selects one Shot of the Scene.
func ScenePage(svc *service.PlanService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Hierarchy(c.Request.Context(), c.Param("id"), c.Param("sceneId"), c.Query("shot"))
		if err != nil {
			pageError(c, err)
			return
		}
		data := sceneShotData(view)
		if isHTMX(c) {
			render(c, http.StatusOK, sceneshot.View(data))
			return
		}
		render(c, http.StatusOK, pages.SceneShotPage(middleware.CSRFToken(c), data))
	}
}

// CreateScene adds a Scene (POST /projects/:id/scenes): answers the Scene form and the new Scene, not the
// whole view, so the Shot form keeps its input (#733). Invalid input (422): the form with the error.
func CreateScene(svc *service.PlanService) gin.HandlerFunc {
	return func(c *gin.Context) {
		form := service.SceneForm{
			Title: c.PostForm("title"), Location: c.PostForm("location"), Time: c.PostForm("time"),
			Objective: c.PostForm("objective"), CurrentSceneID: c.PostForm("current_scene_id"),
		}
		view, err := svc.CreateScene(c.Request.Context(), c.Param("id"), form)
		if err == nil {
			render(c, http.StatusCreated, sceneshot.SceneAdded(sceneShotData(view), form.CurrentSceneID))
			return
		}
		if invalid := asValidation(err); invalid != nil {
			data := sceneShotData(view)
			data.SceneErrors = map[string]string{invalid.Field: invalid.Message}
			data.SceneInput = sceneshot.SceneInput{Title: form.Title, Location: form.Location, Time: form.Time, Objective: form.Objective}
			render(c, http.StatusUnprocessableEntity, sceneshot.SceneRejected(data, form.CurrentSceneID))
			return
		}
		toast(c, err)
	}
}

// CreateShot adds a Shot to a Scene (POST /scenes/:id/shots): answers the Shot form, the new Shot and the
// Shot count, not the whole view, so the Scene form keeps its input (#733). Invalid input (422): the form.
func CreateShot(svc *service.PlanService) gin.HandlerFunc {
	return func(c *gin.Context) {
		form := service.ShotForm{Objective: c.PostForm("objective"), DurationSeconds: c.PostForm("duration_seconds")}
		view, err := svc.CreateShot(c.Request.Context(), c.Param("id"), form)
		if err == nil {
			render(c, http.StatusCreated, sceneshot.ShotAdded(sceneShotData(view)))
			return
		}
		if invalid := asValidation(err); invalid != nil {
			data := sceneShotData(view)
			data.ShotErrors = map[string]string{invalid.Field: invalid.Message}
			data.ShotInput = sceneshot.ShotInput{Objective: form.Objective, DurationSeconds: form.DurationSeconds}
			render(c, http.StatusUnprocessableEntity, sceneshot.ShotRejected(data))
			return
		}
		toast(c, err)
	}
}

func sceneShotData(v service.SceneView) sceneshot.Data {
	d := sceneshot.Data{Project: v.Project, Scene: v.Scene}
	for _, s := range v.Scenes {
		d.Scenes = append(d.Scenes, sceneshot.Item{Scene: s.Scene, ShotCount: s.ShotCount})
	}
	for _, sh := range v.Shots {
		d.Shots = append(d.Shots, molecules.ShotCardData{
			ProjectID: v.Project.ID, Shot: sh.Shot, Storyboard: sh.Storyboard, Takes: sh.Takes, Selected: sh.Selected, Repairable: sh.Repairable, Judgeable: sh.Judgeable, Candidates: sh.Candidates,
		})
	}
	return d
}
