package local

import (
	"errors"
	"net/http"
	"net/url"
	"slices"

	"github.com/gin-gonic/gin"

	service "github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/local"
)

// Handlers answer the routes of the category (router/settingsroutes registers them).
type Handlers struct {
	// Local is the endpoints and models service.
	Local *service.Service
	// Category answers with the page of the category and Endpoint with the page of the endpoint :name: the forms are
	// the answer of a request that is not HTMX (an address typed in, a reload) when it asks for one of them, so that it
	// is a page and not a bare fragment (spec/components/settings-screen-routes.md §6 question 2).
	Category, Endpoint gin.HandlerFunc
}

// notFound is what a request for an endpoint or model that is not there is told.
const notFound = "その接続先またはモデルは見つかりません。画面を読み込み直してください"

// refuse says why a change of the modal did not happen: the modal again, holding what was typed (the key is not), or only
// the reason when there is nothing to correct (the file is pinned, the endpoint is gone). The status is the one classify
// finds; exists is what a name in use is told.
func (h Handlers) refuse(c *gin.Context, err error, exists string, f view.EndpointForm) {
	status, msg := h.classify(c, err, exists)
	if status == http.StatusNotFound || errors.Is(err, service.ErrPinned) {
		settingshttp.Render(c, status, modal.Notice(formTitle(f), msg))
		return
	}
	settingshttp.Render(c, status, view.EndpointModal(f, msg))
}

func formTitle(f view.EndpointForm) string {
	if f.Edit {
		return "接続先を編集"
	}
	return "接続先を追加"
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
		return http.StatusNotFound, notFound
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, invalid.Message
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "保存できませんでした。保存先の権限と空き容量、mam のログを確認してください"
}

// unchangeable is why no form can be opened: the file is pinned (409) or cannot be read (500). Status 0 means it can.
func (h Handlers) unchangeable() (int, string) {
	switch d := SectionData(h.Local); {
	case d.Problem != "":
		return http.StatusInternalServerError, d.Problem
	case d.Pinned:
		return http.StatusConflict, pinnedError
	}
	return 0, ""
}

// endpointOf finds a saved endpoint as the screen shows it (with its models).
func (h Handlers) endpointOf(name string) (view.Endpoint, bool) {
	d := SectionData(h.Local)
	i := slices.IndexFunc(d.Endpoints, func(e view.Endpoint) bool { return e.Name == name })
	if i < 0 {
		return view.Endpoint{}, false
	}
	return d.Endpoints[i], true
}

// kindOf is the kind of the saved endpoint called name, or ErrNotFound.
func (h Handlers) kindOf(name string) (service.Kind, error) {
	v, err := h.Local.View()
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(v.Endpoints, func(e service.EndpointView) bool { return e.Name == name })
	if i < 0 {
		return "", service.ErrNotFound
	}
	return v.Endpoints[i].Kind, nil
}

// postForm is the submitted form (urlencoded bodies are parsed by the body limit already).
func postForm(c *gin.Context) url.Values {
	_ = c.Request.ParseForm()
	return c.Request.PostForm
}
