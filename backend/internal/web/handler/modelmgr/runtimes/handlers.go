// Package runtimes holds the handlers of the section "ローカル音声・口パク" of the model manager screen (spec/api/endpoints-model-manager.md):
// the cards of the local voice and lip-sync runtimes, their install (the package and the weights), start, stop, autostart and removal.
// It is a package of its own because the handler/modelmgr package is at its size limit. Every action calls localruntime.Service, which
// enforces the rules (the machine, the weights, the free space); the screen only asks.
package runtimes

import (
	"context"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	card "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/runtimecard"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the routes of the section.
type Handlers struct {
	svc *localruntime.Service
}

// Register adds the routes of the section to the engine.
func Register(engine *gin.Engine, svc *localruntime.Service) {
	h := Handlers{svc: svc}
	engine.GET(paths.ModelManagerPart("runtimes", ":id"), h.Runtime)
	engine.POST(paths.ModelManagerPart("runtimes", ":id", "install"), h.InstallRuntime)
	engine.POST(paths.ModelManagerPart("runtimes", ":id", "weights"), h.InstallRuntimeWeights)
	engine.POST(paths.ModelManagerPart("runtimes", ":id", "start"), h.StartRuntime)
	engine.POST(paths.ModelManagerPart("runtimes", ":id", "stop"), h.StopRuntime)
	engine.PUT(paths.ModelManagerPart("runtimes", ":id", "autostart"), h.SetRuntimeAutoStart)
	engine.DELETE(paths.ModelManagerPart("runtimes", ":id"), h.RemoveRuntime)
}

// Cancel stops the running install of a runtime (the cancel button of its card, POST /model-manager/jobs/:job/cancel, which the
// handler of the screen routes here for a job whose target is a runtime) and answers with the card.
func Cancel(c *gin.Context, svc *localruntime.Service, runtimeID, jobID string) {
	Handlers{svc: svc}.cancelRuntimeJob(c, runtimeID, jobID)
}

// refuseRuntime is refuse for the errors of the local runtimes: the same statuses (409 when the state does not allow it, 404 for what is
// not there), the messages of mmview.RefuseRuntime; a fault is a generic 500 with the cause in the log.
func refuseRuntime(c *gin.Context, err error) (status int, msg string) {
	kind, msg := mmview.RefuseRuntime(err)
	switch kind {
	case mmview.RefusalInvalid:
		return http.StatusUnprocessableEntity, msg
	case mmview.RefusalConflict:
		return http.StatusConflict, msg
	case mmview.RefusalNotFound:
		return http.StatusNotFound, msg
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "処理できませんでした。mam のログを確認してください"
}

// runtimeCard answers an action of a runtime with its card. A runtime that is not there is a 404 with the section's message.
func (h Handlers) runtimeCard(c *gin.Context, status int, id, job string, starting bool, edit func(*card.Runtime)) {
	r, err := runtimeData(c.Request.Context(), h.svc, id, job, starting)
	if err != nil {
		st, msg := refuseRuntime(c, err)
		render(c, st, card.RuntimesSection(card.Runtimes{Message: view.Message{Error: msg}, Items: items(c.Request.Context(), h.svc)}))
		return
	}
	if edit != nil {
		edit(&r)
	}
	render(c, status, card.RuntimeCard(r))
}

// failed answers a refused action: the card with the reason (409 / 404).
func (h Handlers) failed(c *gin.Context, id string, err error) {
	status, msg := refuseRuntime(c, err)
	h.runtimeCard(c, status, id, "", false, func(r *card.Runtime) { r.Error = msg })
}

// Runtime answers GET /model-manager/runtimes/:id, which the card polls while an install or a start is in progress. job is the
// install it reports on: once that ended, the card says how.
func (h Handlers) Runtime(c *gin.Context) {
	h.runtimeCard(c, http.StatusOK, c.Param("id"), c.Query("job"), false, nil)
}

// InstallRuntime starts the download of the runtime's package and weights as a job (POST /model-manager/runtimes/:id/install) and
// answers with the card showing its progress. A machine that cannot run it, a place that cannot be written to and a full disk are refused
// with the reason (the card with the message).
func (h Handlers) InstallRuntime(c *gin.Context) {
	id := c.Param("id")
	ref, err := h.svc.Install(c.Request.Context(), id)
	if err != nil {
		h.failed(c, id, err)
		return
	}
	h.runtimeCard(c, http.StatusAccepted, id, ref.JobID, false, func(r *card.Runtime) {
		r.Error, r.Notice = "", r.Name+" の導入を始めました。この画面を閉じても続きます"
	})
}

// InstallRuntimeWeights checks the weights and dependencies of an installed runtime and fetches what is missing, as a job
// (POST /model-manager/runtimes/:id/weights).
func (h Handlers) InstallRuntimeWeights(c *gin.Context) {
	id := c.Param("id")
	ref, err := h.svc.InstallWeights(c.Request.Context(), id)
	if err != nil {
		h.failed(c, id, err)
		return
	}
	h.runtimeCard(c, http.StatusAccepted, id, ref.JobID, false, func(r *card.Runtime) {
		r.Error, r.Notice = "", "重みの確認と取得を始めました。この画面を閉じても続きます"
	})
}

// StartRuntime starts the runtime's server (POST /model-manager/runtimes/:id/start) and answers with the card, which polls until it answers
// or shows why it did not start. A refusal that is known now (another machine, not installed, weights missing) is answered at once; the start
// itself (it waits for the models to load) runs on after the answer.
func (h Handlers) StartRuntime(c *gin.Context) {
	ctx, id := c.Request.Context(), c.Param("id")
	if err := h.svc.CheckStart(ctx, id); err != nil {
		h.failed(c, id, err)
		return
	}
	if v, _ := h.svc.Status(ctx, id); v.Process.State == "stopped" || v.Process.State == "failed" {
		go func() {
			if _, err := h.svc.Start(context.WithoutCancel(ctx), id); err != nil {
				h.svc.Logger.Warn("start the local runtime failed", "runtime", id, "err", err) // the card shows it: the process is failed, with the reason
			}
		}()
	}
	h.runtimeCard(c, http.StatusAccepted, id, "", true, nil)
}

// StopRuntime stops the runtime's server (POST /model-manager/runtimes/:id/stop).
func (h Handlers) StopRuntime(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Stop(id); err != nil {
		h.failed(c, id, err)
		return
	}
	h.runtimeCard(c, http.StatusOK, id, "", false, func(r *card.Runtime) { r.Notice = r.Name + " を停止しました" })
}

// SetRuntimeAutoStart saves whether the runtime starts with mam (PUT /model-manager/runtimes/:id/autostart, field on = 1 or 0).
func (h Handlers) SetRuntimeAutoStart(c *gin.Context) {
	id, on := c.Param("id"), c.PostForm(view.FieldOn) == "1"
	if err := h.svc.SetAutoStart(id, on); err != nil {
		h.failed(c, id, err)
		return
	}
	h.runtimeCard(c, http.StatusOK, id, "", false, func(r *card.Runtime) {
		if on {
			r.Notice = "mam の起動時に " + r.Name + " を起動します"
		} else {
			r.Notice = r.Name + " は、最初に使うときに起動します"
		}
	})
}

// RemoveRuntime deletes the runtime with its weights (DELETE /model-manager/runtimes/:id). An install in progress is 409.
func (h Handlers) RemoveRuntime(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Remove(c.Request.Context(), id); err != nil {
		h.failed(c, id, err)
		return
	}
	h.runtimeCard(c, http.StatusOK, id, "", false, func(r *card.Runtime) { r.Notice = r.Name + " を削除しました" })
}

// cancelRuntimeJob stops the running install of a runtime (the cancel button of its card, POST /model-manager/jobs/:job/cancel) and
// answers with the card. The job ends as failed with a message that says it was stopped; installing again continues.
func (h Handlers) cancelRuntimeJob(c *gin.Context, id, jobID string) {
	if err := h.svc.CancelInstall(jobID); err != nil {
		h.failed(c, id, err)
		return
	}
	h.runtimeCard(c, http.StatusOK, id, jobID, false, func(r *card.Runtime) { r.Notice = "導入を中断しています" })
}

func render(c *gin.Context, status int, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}
}
