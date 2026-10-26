// Package workflows holds the handlers of the node screen (GET /nodes: the ComfyUI node UI in an iframe) and of
// the screen of the ComfyUI workflows saved in mam (GET /workflows) with its import, edit, check and delete
// actions (spec/api/endpoints-workflows.md, spec/architecture/comfy/comfy-embedding.md, comfy-workflows.md). It is a
// package of its own because handler is at its size limit; like handler it depends on the service layer and
// the web layer only.
package workflows

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	service "github.com/ousiassllc/mam/backend/internal/service/workflows"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/workflows"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the routes (router registers them). ModelManagerURL is the model manager screen the node screen
// links to when the bundled ComfyUI is not installed ("" leaves the link out).
type Handlers struct {
	Svc             *service.Service
	ModelManagerURL string
}

// The form limits the name to what the service accepts.
var _ = [1]struct{}{}[view.MaxNameLen-service.MaxNameRunes]

// maxImportBytes bounds the multipart body of an import: the largest graph plus the form.
const maxImportBytes = 4 << 20

// NodesPage answers GET /nodes (and ?endpoint=<name>). The response carries frame-src for the origin of the ComfyUI
// it shows, and for that origin only (middleware.AllowFrame); a ComfyUI that cannot be shown gets no frame-src.
func (h Handlers) NodesPage(c *gin.Context) {
	opened := c.Query("opened")
	if !strings.HasPrefix(opened, "mam/") || !strings.HasSuffix(opened, ".json") || len(opened) > 200 {
		opened = ""
	}
	h.nodes(c, opened, "")
}

// StartNodes answers POST /nodes/start: it starts the bundled ComfyUI and answers with the node screen (an
// HX-Redirect to GET /nodes, so that the page is drawn with the frame the policy allows).
func (h Handlers) StartNodes(c *gin.Context) {
	if err := h.Svc.Start(c.Request.Context()); err != nil {
		_, msg := classify(c, err)
		h.nodes(c, "", msg)
		return
	}
	c.Header("HX-Redirect", paths.Nodes())
	c.Status(http.StatusNoContent)
}

func (h Handlers) nodes(c *gin.Context, opened, errMsg string) {
	endpoint := c.Query(view.FieldEndpoint)
	t := h.Svc.NodeTarget(c.Request.Context(), c.Request.Host, endpoint)
	d := view.NodeData{
		Endpoint: t.Endpoint, Endpoints: h.Svc.Endpoints(), Bundled: t.Bundled, State: string(t.State), Reason: t.Reason,
		FrameURL: t.FrameURL, OpenURL: t.OpenURL, Installed: t.Installed, Opened: opened, ModelManagerURL: h.ModelManagerURL, Error: errMsg,
	}
	if t.State == service.NodeReady && !middleware.AllowFrame(c, t.Origin) {
		d.State, d.Reason, d.FrameURL = string(service.NodeFailed), "ComfyUI のアドレスを表示に使えません: "+t.Origin, ""
	}
	status := http.StatusOK
	if errMsg != "" {
		status = http.StatusUnprocessableEntity
	}
	render(c, status, pages.NodesPage(middleware.CSRFToken(c), d))
}

// Page answers GET /workflows: the page, or the panel alone for an HTMX request ("閉じる").
func (h Handlers) Page(c *gin.Context) {
	h.respond(c, http.StatusOK, h.data(c.Request.Context()))
}

// respond draws d: the whole page for a plain request, the panel for an HTMX one.
func (h Handlers) respond(c *gin.Context, status int, d view.Data) {
	if c.GetHeader("HX-Request") == "true" && c.GetHeader("HX-History-Restore-Request") != "true" {
		render(c, status, view.Panel(d))
		return
	}
	render(c, status, pages.WorkflowsPage(middleware.CSRFToken(c), d))
}

func (h Handlers) data(ctx context.Context) view.Data {
	d := view.Data{Endpoints: h.Svc.Endpoints()}
	list, err := h.Svc.List(ctx)
	if err != nil {
		d.Problem = "ワークフローを読み込めません。mam のログを確認してください"
		return d
	}
	for _, w := range list {
		d.Workflows = append(d.Workflows, rowOf(w))
	}
	return d
}

func rowOf(w service.Workflow) view.Row {
	return view.Row{
		ID: w.ID, Name: w.Name, Kind: string(w.Kind), Endpoint: w.Endpoint, ModelID: w.ModelID, Version: w.Version,
		Updated: w.UpdatedAt.Local().Format("2006-01-02 15:04"), Ready: w.Ready, Problems: w.Problems, HasUI: w.HasUI, UsedBy: w.UsedBy,
	}
}

// ok answers a change that worked: the panel with the notice.
func (h Handlers) ok(c *gin.Context, notice string) {
	d := h.data(c.Request.Context())
	d.Notice = notice
	render(c, http.StatusOK, view.Panel(d))
}

// refuse answers a change that was refused: the panel with the reason (and the form that was open).
func (h Handlers) refuse(c *gin.Context, err error, d view.Data) {
	status, msg := classify(c, err)
	d.Error = msg
	render(c, status, view.Panel(d))
}

// classify maps the service's refusals: ErrExists to 409, ErrNotFound to 404, *InvalidError to 422 with its
// message; anything else is a 500 with a generic message (the cause goes to the log).
func classify(c *gin.Context, err error) (int, string) {
	var invalid *service.InvalidError
	switch {
	case errors.Is(err, service.ErrExists):
		return http.StatusConflict, "その名前のワークフローはすでにあります。別の名前にしてください"
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, "そのワークフローは見つかりません。画面を読み込み直してください"
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, invalid.Message
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "処理できませんでした。mam のログと ComfyUI の状態を確認してください"
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

// postForm is the submitted urlencoded form.
func postForm(c *gin.Context) url.Values {
	_ = c.Request.ParseForm()
	return c.Request.PostForm
}

func first(form url.Values, name string) string {
	if v := form[name]; len(v) > 0 {
		return strings.TrimSpace(v[0])
	}
	return ""
}
