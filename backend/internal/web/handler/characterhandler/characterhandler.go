// Package characterhandler holds the handlers that make a Character by hand (#1001): in the World State of a Project and in the canon of
// a Series (spec/api/endpoints.md §2.8 and §2.9). It is a package of its own because handler is at its size limit.
package characterhandler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
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

// SeriesData is the view of the Series (GET /series/:id, and the answer of the making of a Character) with the images the create form offers as
// the face or the body: the Library images (kind character) of its Episodes, each labelled with its Episode (a Series has no Library of its
// own; none, no select). The value of an option is "<project id>/<entry id>" (splitImage). An unknown Series is service.ErrNotFound.
func SeriesData(ctx context.Context, svc *service.Services, seriesID string) (seriesview.Data, error) {
	v, err := svc.Series.View(ctx, seriesID)
	if err != nil {
		return seriesview.Data{}, err
	}
	data := seriesview.Data{Series: v.Series, Canon: v.Canon, Versions: v.Versions, Episodes: v.Episodes, Templates: v.Templates}
	episodes, err := svc.VoiceRef.SeriesImages(ctx, seriesID)
	if err != nil {
		return seriesview.Data{}, err
	}
	for _, e := range episodes {
		for _, it := range e.Items {
			data.Images = append(data.Images, molecules.CharacterImageOption{Value: e.Project.ID + "/" + it.ID, Label: "第" + strconv.Itoa(e.Project.EpisodeNumber) + "話 " + e.Project.Name + " / " + it.Name})
		}
	}
	return data, nil
}

// splitImage is the Project and the Library entry of an option value of SeriesData ("", "" for no image); ok is false for a value of another form.
func splitImage(value string) (projectID, entryID string, ok bool) {
	if value = strings.TrimSpace(value); value == "" {
		return "", "", true
	}
	projectID, entryID, ok = strings.Cut(value, "/")
	return projectID, entryID, ok && projectID != "" && entryID != ""
}

// seriesCharacter reads the form of a canonical Character: the face and the body are options of SeriesData, which must be of the same
// Episode (the canon takes the images of one Project, voicerefsvc.CreateForSeries). It returns the Project the images are of.
func seriesCharacter(c *gin.Context) (in voicerefsvc.NewCharacter, referenceProjectID string, err error) {
	in = newCharacter(c)
	faceProject, face, faceOK := splitImage(in.FaceReferenceID)
	bodyProject, body, bodyOK := splitImage(in.BodyReferenceID)
	in.FaceReferenceID, in.BodyReferenceID = face, body
	switch {
	case !faceOK:
		return in, "", &voicerefsvc.InvalidError{Field: "face_reference_id", Message: "顔の参照画像を選び直してください"}
	case !bodyOK:
		return in, "", &voicerefsvc.InvalidError{Field: "body_reference_id", Message: "全身の参照画像を選び直してください"}
	case face != "" && body != "" && faceProject != bodyProject:
		return in, "", &voicerefsvc.InvalidError{Field: "body_reference_id", Message: "顔と全身の参照画像は、同じ Episode のものを選んでください"}
	case face != "":
		return in, faceProject, nil
	}
	return in, bodyProject, nil
}

// CreateInSeries makes a Character in the canon of the Series (POST /series/:id/characters; the form is the same, its face and body
// options are the Library images of the Episodes) and answers (201) with an empty form, the card of the new Character and the new
// version. Existing Episodes are not changed. A name that is empty, too long or taken, or images of two Episodes, is 422; an unknown
// Series or Library entry is 404.
func CreateInSeries(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		in, project, err := seriesCharacter(c)
		if err != nil {
			fail(c, err)
			return
		}
		id, err := svc.VoiceRef.CreateForSeries(ctx, c.Param("id"), project, in)
		if err != nil {
			fail(c, err)
			return
		}
		data, err := SeriesData(ctx, svc, c.Param("id"))
		if err != nil {
			fail(c, err)
			return
		}
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
