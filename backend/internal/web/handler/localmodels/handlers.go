package localmodels

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	service "github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	modelassign "github.com/ousiassllc/mam/backend/internal/web/handler/modelassign"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/localmodels"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelassignment"
)

// Handlers answer the routes of the section (router/settingsroutes registers them).
type Handlers struct {
	// Local is the endpoints and models service.
	Local *service.Service
	// Models, when set, is the image / video assignment: its form is drawn again (out of band) after a change,
	// so that an added or removed model is a choice there at once.
	Models *modelsettings.Service
}

// Panel answers GET /settings/local-models with the section alone: what "閉じる" swaps in.
func (h Handlers) Panel(c *gin.Context) {
	h.respond(c, http.StatusOK, SectionData(h.Local), false)
}

// respond draws the panel d. changed: the settings were changed, so the assignment form is refreshed too.
func (h Handlers) respond(c *gin.Context, status int, d view.Data, changed bool) {
	render(c, status, templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := view.Panel(d).Render(ctx, w); err != nil {
			return err
		}
		if !changed || h.Models == nil {
			return nil
		}
		md := modelassign.SectionData(h.Models)
		if md.Problem != "" {
			return nil
		}
		md.OOB = true
		return modelassignment.Form(md).Render(ctx, w)
	}))
}

// ok answers a change that worked: the panel with the notice, 200.
func (h Handlers) ok(c *gin.Context, notice string) {
	d := SectionData(h.Local)
	d.Notice = notice
	h.respond(c, http.StatusOK, d, true)
}

// refuse answers a change that did not work: the panel with the reason, and the form (open) holding what
// was submitted. status is 409 for a pinned file or a name in use, 404, 422 for a refused input, 500.
func (h Handlers) refuse(c *gin.Context, err error, exists string, editor view.Editor) {
	status, msg := h.classify(c, err, exists)
	d := SectionData(h.Local)
	switch {
	case status == http.StatusConflict && errors.Is(err, service.ErrPinned):
		// nothing to correct in a form: the file cannot be changed
	case editor.Open():
		editor.Error = msg
		d.Editor = editor
		msg = ""
	}
	d.Error = msg
	h.respond(c, status, d, false)
}

// classify maps the service's refusals: ErrPinned and ErrExists to 409, ErrNotFound to 404 and *InvalidError
// to 422 with its message; anything else is a 500 with a generic message (the cause goes to the log).
func (h Handlers) classify(c *gin.Context, err error, exists string) (int, string) {
	var invalid *service.InvalidError
	switch {
	case errors.Is(err, service.ErrPinned):
		return http.StatusConflict, pinnedError
	case errors.Is(err, service.ErrExists):
		return http.StatusConflict, exists
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, "その接続先またはモデルは見つかりません。画面を読み込み直してください"
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, explain(invalid.Message)
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "保存できませんでした。保存先の権限と空き容量、mam のログを確認してください"
}

// linkLocalHint is added to a refusal of a link-local (or unspecified, multicast) address, whose reason the
// validation states in English only: it is never allowed, not even for an address the user registers.
const linkLocalHint = "（リンクローカル（169.254.x.x・fe80::）、未指定、マルチキャストのアドレスは、登録した接続先でも使えません。クラウドのメタデータサービスなどに届いてしまうためです）"

func explain(msg string) string {
	if strings.Contains(msg, "link-local") {
		return msg + " " + linkLocalHint
	}
	return msg
}

// open answers a button that opens a form: the form alone (it is swapped into #local-models-editor). A pinned
// file has no form: the panel (swapped in place of itself) says why.
func (h Handlers) open(c *gin.Context, editor view.Editor) {
	d := SectionData(h.Local)
	if d.Problem == "" && !d.Pinned {
		d.Editor = editor
		render(c, http.StatusOK, view.EditorBody(d))
		return
	}
	status := http.StatusInternalServerError
	if d.Pinned {
		status, d.Error = http.StatusConflict, pinnedError
	}
	c.Header("HX-Retarget", "#"+view.PanelID)
	c.Header("HX-Reswap", "outerHTML")
	h.respond(c, status, d, false)
}

// endpointOf finds a saved endpoint.
func (h Handlers) endpointOf(name string) (service.EndpointView, bool, error) {
	v, err := h.Local.View()
	if err != nil {
		return service.EndpointView{}, false, err
	}
	i := slices.IndexFunc(v.Endpoints, func(e service.EndpointView) bool { return e.Name == name })
	if i < 0 {
		return service.EndpointView{}, false, nil
	}
	return v.Endpoints[i], true, nil
}

// kindOf is the kind of the saved endpoint called name, or ErrNotFound.
func (h Handlers) kindOf(name string) (service.Kind, error) {
	e, found, err := h.endpointOf(name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", service.ErrNotFound
	}
	return e.Kind, nil
}

// postForm is the submitted form (urlencoded bodies are parsed by the body limit already).
func postForm(c *gin.Context) url.Values {
	_ = c.Request.ParseForm()
	return c.Request.PostForm
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
