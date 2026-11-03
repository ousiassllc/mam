// Package characterhandler holds the handlers that make a Character by hand (#1001): in the World State of a Project and in the canon of
// a Series (spec/api/endpoints.md §2.8 and §2.9). It is a package of its own because handler is at its size limit.
package characterhandler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/seriesview"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/worldstate"
)

// newCharacter reads the form: name, hairstyle, wardrobe, personality, voice and the Library entries of the face and the body.
func newCharacter(c *gin.Context) voicerefsvc.NewCharacter {
	return voicerefsvc.NewCharacter{
		Name: c.PostForm("name"), Hairstyle: c.PostForm("hairstyle"), Wardrobe: c.PostForm("wardrobe"), Voice: c.PostForm("voice"), Personality: c.PostForm("personality"),
		FaceReferenceID: c.PostForm("face_reference_id"), BodyReferenceID: c.PostForm("body_reference_id"),
	}
}

// CreateInProject makes a Character in the World State of the Project (POST /projects/:id/characters; the query branch_id selects a
// Branch) and answers (201) with an empty form, the forms of the new Character and the new version. A name that is empty, too long or
// taken, a text that is too long or a Library image of the wrong kind is 422; an unknown Project, Branch or Library entry is 404.
func CreateInProject(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, branchID := c.Request.Context(), c.Query("branch_id")
		id, err := svc.VoiceRef.CreateForProject(ctx, c.Param("id"), branchID, newCharacter(c))
		if err != nil {
			fail(c, err)
			return
		}
		view, err := svc.World.View(ctx, c.Param("id"), branchID)
		if err != nil {
			fail(c, err)
			return
		}
		data := worldstate.Data{Project: view.Project, Branch: view.Branch, World: view.World, Locks: view.Locks, ProjectLocks: view.ProjectLocks, Library: view.Library, Versions: view.Versions}
		settingshttp.Render(c, http.StatusCreated, worldstate.CharacterCreated(data, id))
	}
}

// CreateInSeries makes a Character in the canon of the Series (POST /series/:id/characters; the form is the same without the images: a
// Series has no Reference Library) and answers (201) with an empty form, the card of the new Character and the new version. Existing
// Episodes are not changed. A name that is empty, too long or taken is 422; an unknown Series is 404.
func CreateInSeries(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		in := newCharacter(c)
		in.FaceReferenceID, in.BodyReferenceID = "", ""
		id, err := svc.VoiceRef.CreateForSeries(c.Request.Context(), c.Param("id"), "", in)
		if err != nil {
			fail(c, err)
			return
		}
		v, err := svc.Series.View(c.Request.Context(), c.Param("id"))
		if err != nil {
			fail(c, err)
			return
		}
		data := seriesview.Data{Series: v.Series, Canon: v.Canon, Versions: v.Versions, Episodes: v.Episodes, Templates: v.Templates}
		settingshttp.Render(c, http.StatusCreated, seriesview.CharacterCreated(data, id))
	}
}

// fail answers with the toast of the error, as handler.toast does for the other screens.
func fail(c *gin.Context, err error) {
	var invalid *voicerefsvc.InvalidError
	switch {
	case errors.As(err, &invalid):
		settingshttp.Toast(c, http.StatusUnprocessableEntity, invalid.Message)
	case errors.Is(err, voicerefsvc.ErrNotFound), errors.Is(err, service.ErrNotFound):
		settingshttp.Toast(c, http.StatusNotFound, "対象が見つかりません")
	default:
		_ = c.Error(err)
		settingshttp.Toast(c, http.StatusInternalServerError, "内部エラーが発生しました")
	}
}
