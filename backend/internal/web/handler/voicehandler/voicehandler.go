// Package voicehandler holds the handlers of the reference voice of a Character (F-20, F-29): the upload, the
// removal and the playback, for the canonical Characters of a Series and for the Characters of a Project
// (spec/api/endpoints.md §2.9). It is a package of its own because handler is at its size limit.
package voicehandler

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/seriesview"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/worldstate"
)

// multipartMemory is how much of an upload Go keeps in memory before spilling to temp files; the limit of the
// body is the recording's own and the form fields around it.
const (
	multipartMemory = 8 << 20
	bodyLimit       = voiceref.MaxBytes + 1<<20
)

// SetSeries sets the reference voice of a canonical Character (PUT /series/:id/characters/:characterId/voice-reference,
// multipart file / text / language) and answers with the voice form of that Character, with the new version of the
// canon carried along. Existing Episodes keep their voice.
func SetSeries(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		in, err := readInput(c)
		if err == nil {
			_, err = svc.VoiceRef.SetForSeries(c.Request.Context(), c.Param("id"), c.Param("characterId"), in)
		}
		answerSeries(c, svc, err)
	}
}

// RemoveSeries removes it (DELETE /series/:id/characters/:characterId/voice-reference) and answers the same.
func RemoveSeries(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		answerSeries(c, svc, svc.VoiceRef.RemoveForSeries(c.Request.Context(), c.Param("id"), c.Param("characterId")))
	}
}

func answerSeries(c *gin.Context, svc *service.Services, err error) {
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
	settingshttp.Render(c, http.StatusOK, seriesview.VoiceSaved(data, c.Param("characterId")))
}

// SetProject sets the reference voice of a Character of the Project (PUT /projects/:id/characters/:characterId/voice-reference;
// the query branch_id names the Branch the view is of) and answers with the voice form of that Character.
func SetProject(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		in, err := readInput(c)
		if err == nil {
			_, err = svc.VoiceRef.SetForProject(c.Request.Context(), c.Param("id"), c.Param("characterId"), in)
		}
		answerProject(c, svc, err)
	}
}

// RemoveProject removes it (DELETE /projects/:id/characters/:characterId/voice-reference) and answers the same.
func RemoveProject(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		answerProject(c, svc, svc.VoiceRef.RemoveForProject(c.Request.Context(), c.Param("id"), c.Param("characterId")))
	}
}

func answerProject(c *gin.Context, svc *service.Services, err error) {
	if err != nil {
		fail(c, err)
		return
	}
	v, err := svc.World.View(c.Request.Context(), c.Param("id"), c.Query("branch_id"))
	if err != nil {
		fail(c, err)
		return
	}
	data := worldstate.Data{Project: v.Project, Branch: v.Branch, World: v.World, Locks: v.Locks, ProjectLocks: v.ProjectLocks, Library: v.Library, Versions: v.Versions}
	settingshttp.Render(c, http.StatusOK, worldstate.VoiceSaved(data, c.Param("characterId")))
}

// AudioSeries serves the recording of a canonical Character (GET /series/:id/characters/:characterId/voice-reference);
// AudioProject that of a Character of the Project. 404 for a Character that has none.
func AudioSeries(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		serve(c)(svc.VoiceRef.PlaybackForSeries(c.Request.Context(), c.Param("id"), c.Param("characterId")))
	}
}

// AudioProject serves the recording of a Character of the Project (GET /projects/:id/characters/:characterId/voice-reference).
func AudioProject(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		serve(c)(svc.VoiceRef.PlaybackForProject(c.Request.Context(), c.Param("id"), c.Param("characterId")))
	}
}

// serve answers with the file; the file's own type is declared and the page is sandboxed, like any uploaded file.
func serve(c *gin.Context) func(voicerefsvc.Playback, error) {
	return func(p voicerefsvc.Playback, err error) {
		if err != nil {
			status, _ := statusOf(c, err)
			c.AbortWithStatus(status)
			return
		}
		c.Header("Content-Type", p.MimeType)
		c.Header("Content-Security-Policy", "sandbox")
		c.Header("X-Content-Type-Options", "nosniff")
		c.File(p.Path)
	}
}

// readInput reads the multipart form: the optional file and the optional text and language (a field that was not
// sent keeps what the Character has; the transcript the form sends back unchanged does not stay with a new recording).
func readInput(c *gin.Context) (voicerefsvc.Input, error) {
	if c.Request.ContentLength > bodyLimit {
		return voicerefsvc.Input{}, errTooLarge
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, bodyLimit)
	if err := c.Request.ParseMultipartForm(multipartMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return voicerefsvc.Input{}, errTooLarge
		}
		return voicerefsvc.Input{}, &voicerefsvc.InvalidError{Message: "送信内容を読み取れませんでした（multipart/form-data で送ってください）"}
	}
	form := c.Request.MultipartForm
	in := voicerefsvc.Input{TextPrefilled: true}                               // the voice form pre-fills the transcript field with the stored one
	if files := form.File["file"]; len(files) > 0 && files[0].Filename != "" { // an empty file input is an unnamed part
		in.File = uploadOf(files[0])
	}
	if v, ok := form.Value["text"]; ok && len(v) > 0 {
		in.Text = &v[0]
	}
	if v, ok := form.Value["language"]; ok && len(v) > 0 {
		in.Language = &v[0]
	}
	return in, nil
}

func uploadOf(fh *multipart.FileHeader) voicerefsvc.File {
	return voicerefsvc.File{Name: fh.Filename, Size: fh.Size, Open: func() (io.ReadCloser, error) { return fh.Open() }}
}

var errTooLarge = errors.New("voicehandler: request too large")

// statusOf maps an error to the status and the message the user sees, as handler.statusOf does for the other screens.
func statusOf(c *gin.Context, err error) (int, string) {
	var invalid *voicerefsvc.InvalidError
	switch {
	case errors.Is(err, errTooLarge):
		return http.StatusRequestEntityTooLarge, "送信内容が大きすぎます"
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, invalid.Message
	case errors.Is(err, voicerefsvc.ErrNotFound), errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, "対象が見つかりません"
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "内部エラーが発生しました"
}

func fail(c *gin.Context, err error) {
	status, message := statusOf(c, err)
	settingshttp.Toast(c, status, message)
}
