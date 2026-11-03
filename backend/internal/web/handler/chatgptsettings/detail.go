package chatgptsettings

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/chatgpt"
)

// Detail answers the detail modal of a card (GET /settings/chatgpt/:kind/detail): what is too long for the card. A request that
// is not HTMX gets page; an unknown kind asked by HTMX gets a 404 modal that says so.
func Detail(svc *chatgpt.Service, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		kind := c.Param("kind")
		if !validKind(kind) {
			render(c, http.StatusNotFound, modal.Notice("ChatGPT でログイン", "対象が見つかりません"))
			return
		}
		c.Header("Cache-Control", "no-store")
		render(c, http.StatusOK, view.DetailModal(view.DetailForm{Kind: kind, Data: SectionData(svc)}))
	}
}

// answer is the answer of an action of the cards or of a detail modal. A button of a card swaps the panel; an action of the
// modal (its target is the modal body) closes it with the panel replaced out of band when it worked, and shows why it did not
// in the modal. kind is the card whose detail it was.
func answer(c *gin.Context, status int, d view.Data, kind string) {
	if c.GetHeader("HX-Target") != modal.BodyID {
		render(c, status, view.Panel(d))
		return
	}
	d.OOB = true
	if status < http.StatusBadRequest {
		settingshttp.CloseModal(c)
		render(c, status, view.Panel(d))
		return
	}
	reason := d.Error
	d.Error = ""
	render(c, status, templ.Join(view.DetailModal(view.DetailForm{Kind: kind, Data: d, Error: reason}), view.Panel(d)))
}
