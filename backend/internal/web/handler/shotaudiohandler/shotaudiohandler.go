// Package shotaudiohandler holds the handlers of the audio of a Shot in the Shot Inspector (Issue #1153; spec/api/endpoints.md §2.2): GET /shots/:id/audio is the
// section, PUT /shots/:id/audio ties an audio asset of the Project to the Shot or takes it off. It is a package of its own because handler is at its size limit; like
// characterhandler it turns the refusals of its service (shotaudiosvc, which does not import package service) into answers itself.
package shotaudiohandler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/shotaudiosvc"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/shotinspector/audiopanel"
)

// Data is the audio of the Shot as the Inspector's section shows it.
func Data(v shotaudiosvc.View) audiopanel.Data {
	d := audiopanel.Data{
		ShotID: v.ShotID, ProjectID: v.ProjectID, Locked: v.Locked, Busy: v.Busy, MaxSeconds: v.MaxSeconds, Pictures: v.Pictures,
		Choices: make([]audiopanel.Choice, 0, len(v.Choices)),
	}
	if t := v.Tie; t != nil {
		d.Current = &audiopanel.Current{AssetID: t.Asset.ID, MimeType: t.Asset.MimeType, SizeBytes: t.Asset.SizeBytes, Seconds: t.Seconds, Problem: t.Problem}
	}
	for _, a := range v.Choices {
		d.Choices = append(d.Choices, audiopanel.Choice{AssetID: a.ID, MimeType: a.MimeType, SizeBytes: a.SizeBytes, CreatedAt: a.CreatedAt})
	}
	return d
}

// Get answers the section (GET /shots/:id/audio). An unknown Shot is 404.
func Get(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, err := svc.ShotAudio.Get(c.Request.Context(), "", c.Param("id"))
		if err != nil {
			fail(c, err)
			return
		}
		settingshttp.NoStore(c)
		settingshttp.Render(c, http.StatusOK, audiopanel.Section(Data(v)))
	}
}

// Put ties the audio asset of the form (asset_id) to the Shot, or takes the audio off when asset_id is empty (PUT /shots/:id/audio), and answers with the section
// as it is now (200). An unknown Shot or asset (also one of another Project) is 404; an asset that is no audio file 422; a Shot that is being generated or repaired
// 409; a Shot whose Prompt is LOCKed 423. A refusal is a toast and leaves the section as it was.
func Put(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, err := svc.ShotAudio.Set(c.Request.Context(), "", c.Param("id"), c.PostForm("asset_id"))
		if err != nil {
			fail(c, err)
			return
		}
		settingshttp.Render(c, http.StatusOK, audiopanel.Section(Data(v)))
	}
}

// fail answers with the toast of the refusal, as handler.toast does for the other forms of the Inspector.
func fail(c *gin.Context, err error) {
	var (
		invalid *shotaudiosvc.InvalidError
		locked  *shotaudiosvc.LockedError
	)
	switch {
	case errors.Is(err, shotaudiosvc.ErrNotFound):
		settingshttp.Toast(c, http.StatusNotFound, "Shot または音声が見つかりません")
	case errors.Is(err, shotaudiosvc.ErrBusy):
		settingshttp.Toast(c, http.StatusConflict, "この Shot は生成中か修復中です。終わってから音声を変えてください")
	case errors.As(err, &locked):
		settingshttp.Toast(c, http.StatusLocked, lockedText(locked))
	case errors.As(err, &invalid):
		settingshttp.Toast(c, http.StatusUnprocessableEntity, invalid.Message)
	default:
		_ = c.Error(err)
		settingshttp.Toast(c, http.StatusInternalServerError, "内部エラーが発生しました")
	}
}

func lockedText(e *shotaudiosvc.LockedError) string {
	text := ""
	for i, p := range e.Parameters {
		if i > 0 {
			text += " / "
		}
		text += p.Label()
	}
	return text + " は LOCK されているため、音声を変えられません。LOCK を解除してから変更してください"
}
