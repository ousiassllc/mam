package modelmgr

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/ollama"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/runtimes"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/modeldetail"
)

// Engine answers GET /model-manager/engine, which the card polls while an install or a start is in progress. job is
// the install it reports on: once that ended, the card says how (the model list is on another page and is read when that page is opened).
func (h Handlers) Engine(c *gin.Context) {
	job := c.Query("job")
	e := engineData(c.Request.Context(), h.Svc, job, false)
	render(c, http.StatusOK, view.EngineCard(e))
}

// EngineDetail answers GET /model-manager/engine/detail with the modal of the card: what the engine is, where it comes from, its license, and why it
// cannot run here (the card says none of it).
func (h Handlers) EngineDetail(c *gin.Context) {
	render(c, http.StatusOK, modeldetail.EngineDetail(engineData(c.Request.Context(), h.Svc, "", false)))
}

// GPU answers GET /model-manager/engine/gpu with the detection result and the form that installs the engine.
func (h Handlers) GPU(c *gin.Context) {
	render(c, http.StatusOK, view.GPUBlock(gpuData(c.Request.Context(), h.Svc)))
}

// InstallEngine starts the download of the engine (POST /model-manager/engine/install; fields variant, cpu) as a
// job and answers with the card showing its progress. Without a variant the GPU decides; no GPU is 422 with what to do
// (an external ComfyUI, or cpu=1 as an explicit choice).
func (h Handlers) InstallEngine(c *gin.Context) {
	ctx := c.Request.Context()
	in := modelmanager.EngineInstallInput{Variant: catalog.Variant(strings.TrimSpace(c.PostForm(view.FieldVariant))), CPU: c.PostForm(view.FieldCPU) != ""}
	ref, err := h.Svc.InstallEngine(ctx, in)
	if err != nil {
		status, msg := refuse(c, err)
		e := engineData(ctx, h.Svc, "", false)
		e.Error = msg
		render(c, status, view.EngineCard(e))
		return
	}
	e := engineData(ctx, h.Svc, ref.JobID, false)
	e.Notice = "ComfyUI の導入を始めました。この画面を閉じても続きます"
	render(c, http.StatusAccepted, view.EngineCard(e))
}

// StartEngine starts the engine (POST /model-manager/engine/start) and answers with the card, which polls until
// ComfyUI answers or shows why it did not start. The start itself (it waits for ComfyUI to load) runs on after the
// answer; a refusal that is known now (no engine, not Windows) is answered at once.
func (h Handlers) StartEngine(c *gin.Context) {
	ctx := c.Request.Context()
	switch ev := h.Svc.Engine(); {
	case !ev.Supported:
		h.startRefused(c, modelmanager.ErrUnsupported)
		return
	case !ev.Installed || ev.Broken:
		h.startRefused(c, modelmanager.ErrEngineNotInstalled)
		return
	case ev.Process.State == "stopped" || ev.Process.State == "failed":
		go func() {
			if _, err := h.Svc.StartEngine(context.WithoutCancel(ctx)); err != nil {
				h.Svc.Logger.Warn("start the bundled ComfyUI failed", "err", err) // the card shows it: the process is failed, with the reason
			}
		}()
	}
	render(c, http.StatusAccepted, view.EngineCard(engineData(ctx, h.Svc, "", true)))
}

func (h Handlers) startRefused(c *gin.Context, err error) {
	status, msg := refuse(c, err)
	e := engineData(c.Request.Context(), h.Svc, "", false)
	e.Error = msg
	render(c, status, view.EngineCard(e))
}

// StopEngine stops the engine (POST /model-manager/engine/stop).
func (h Handlers) StopEngine(c *gin.Context) {
	err := h.Svc.StopEngine()
	e := engineData(c.Request.Context(), h.Svc, "", false)
	status := http.StatusOK
	if err != nil {
		status, e.Error = refuse(c, err)
	} else {
		e.Notice = "ComfyUI を停止しました"
	}
	render(c, status, view.EngineCard(e))
}

// SetAutoStart saves whether the engine starts with mam (PUT /model-manager/engine/autostart, field on = 1 or 0).
func (h Handlers) SetAutoStart(c *gin.Context) {
	on := c.PostForm(view.FieldOn) == "1"
	err := h.Svc.SetAutoStart(on)
	e := engineData(c.Request.Context(), h.Svc, "", false)
	status := http.StatusOK
	if err != nil {
		status, e.Error = refuse(c, err)
	} else if on {
		e.Notice = "mam の起動時に ComfyUI を起動します"
	} else {
		e.Notice = "ComfyUI は、モデルを最初に使うときに起動します"
	}
	render(c, status, view.EngineCard(e))
}

// RemoveEngine deletes the engine (DELETE /model-manager/engine): the models stay. An install in progress is 409.
func (h Handlers) RemoveEngine(c *gin.Context) {
	err := h.Svc.RemoveEngine(c.Request.Context())
	e := engineData(c.Request.Context(), h.Svc, "", false)
	status := http.StatusOK
	if err != nil {
		status, e.Error = refuse(c, err)
	} else {
		e.Notice = "ComfyUI エンジンを削除しました（導入したモデルのファイルは残っています）"
	}
	render(c, status, view.EngineCard(e))
}

// DetectComfyUI looks for a ComfyUI that is already running on this machine (POST /model-manager/engine/detect).
func (h Handlers) DetectComfyUI(c *gin.Context) {
	rep := h.Svc.DetectComfyUI(c.Request.Context())
	d := view.Detect{Recommended: rep.Recommended, DesktopHint: rep.Desktop.Exists}
	for _, f := range rep.Found {
		dc := view.DetectedComfy{URL: f.URL, Version: f.Version, Desktop: f.Desktop}
		for _, dev := range f.Devices {
			dc.Devices = append(dc.Devices, dev.Name+"（VRAM "+bytesText(dev.VRAMTotal)+"）")
		}
		d.Found = append(d.Found, dc)
	}
	render(c, http.StatusOK, view.DetectBlock(d))
}

// CancelJob stops a running install (POST /model-manager/jobs/:job/cancel) and answers with the card (or the list) of
// what it was installing. The job ends as failed with a message that says it was stopped; installing again continues.
func (h Handlers) CancelJob(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("job")
	st, stErr := h.Svc.JobStatus(ctx, id)
	if rid, ok := strings.CutPrefix(st.Target, localruntime.TargetPrefix); ok && stErr == nil && h.Runtimes != nil {
		runtimes.Cancel(c, h.Runtimes, rid, id)
		return
	}
	if strings.HasPrefix(st.Target, ollamapull.TargetPrefix) && stErr == nil && h.Ollama != nil {
		ollama.Cancel(c, h.Ollama, h.LLMLocal, id)
		return
	}
	err := h.Svc.CancelInstall(id)
	if stErr == nil && st.Target == "engine" {
		e := engineData(ctx, h.Svc, id, false)
		status := http.StatusOK
		if err != nil {
			status, e.Error = refuse(c, err)
		} else {
			e.Notice = "導入を中断しています"
		}
		render(c, status, view.EngineCard(e))
		return
	}
	cat := catalogData(c, h.Svc, []string{id})
	status := http.StatusOK
	if err != nil {
		status, cat.Error = refuse(c, err)
	} else {
		cat.Notice = "導入を中断しています"
	}
	render(c, status, view.CatalogList(cat))
}
