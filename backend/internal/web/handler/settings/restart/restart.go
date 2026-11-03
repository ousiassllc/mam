// Package restart holds the handlers of the "再起動して反映" panel of the global settings screen (spec/architecture/restart.md):
// the panel's data, its re-read (GET /settings/restart) and the button (POST /settings/restart). It is a package of its own
// because handler is at its size limit; like handler it depends on the service layer and the web layer only.
package restart

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/apprestart"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/restart"
)

// PanelData is the panel as the settings screen shows it: the settings that wait for a restart and a request that waits for
// the work to end. A nil svc has nothing to say.
func PanelData(svc *apprestart.Service) view.Data {
	if svc == nil {
		return view.Data{}
	}
	st := svc.Status()
	d := view.Data{Pending: st.Pending, CanRestart: st.Available, Waiting: st.Waiting, Restarting: st.Restarting}
	switch {
	case st.Restarting:
		d.Notice = "再起動しています。まもなくアプリが閉じて、開き直します。"
	case st.Waiting:
		d.Notice = "作業が終わったら再起動します（" + st.WaitUntil.Local().Format("15:04") + " まで待ちます）。"
	}
	return d
}

// Show answers the panel alone (GET /settings/restart): the page reads it again after a save.
func Show(svc *apprestart.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		settingshttp.NoStore(c)
		settingshttp.Render(c, http.StatusOK, view.Panel(PanelData(svc)))
	}
}

// Restart asks for the restart (POST /settings/restart, form when_busy: "refuse" (the default) or "wait"; "stop" is for the
// MCP tool, never from this screen). The answer is the panel: refused with the work that runs (409), waiting (200) or
// restarting (200, the app quits right after).
func Restart(svc *apprestart.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		settingshttp.NoStore(c)
		when := apprestart.WhenBusy(c.PostForm("when_busy"))
		if when != "" && when != apprestart.WhenBusyRefuse && when != apprestart.WhenBusyWait {
			settingshttp.Render(c, http.StatusUnprocessableEntity, view.Panel(withError(PanelData(svc), "when_busy は refuse か wait です")))
			return
		}
		_, err := svc.Request(c.Request.Context(), apprestart.Options{WhenBusy: when})
		var busy *apprestart.BusyError
		switch {
		case errors.As(err, &busy):
			d := withError(PanelData(svc), "作業が動いているため、再起動していません。終わってからもう一度押すか、「作業が終わったら再起動する」を使ってください。")
			for _, b := range busy.Busy {
				d.Busy = append(d.Busy, b.Message)
			}
			settingshttp.Render(c, http.StatusConflict, view.Panel(d))
		case errors.Is(err, apprestart.ErrUnavailable):
			settingshttp.Render(c, http.StatusServiceUnavailable, view.Panel(withError(PanelData(svc), "このビルドは自分で再起動できません。アプリを終了して、起動し直してください。")))
		case err != nil:
			_ = c.Error(err)
			settingshttp.Render(c, http.StatusInternalServerError, view.Panel(withError(PanelData(svc), "再起動を始められませんでした。ログを確認してください。")))
		default:
			settingshttp.Render(c, http.StatusOK, view.Panel(PanelData(svc)))
		}
	}
}

func withError(d view.Data, msg string) view.Data {
	d.Error = msg
	return d
}
