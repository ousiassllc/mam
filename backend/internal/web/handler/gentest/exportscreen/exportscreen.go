// Package exportscreen holds the handler of the ZIP export of the generation test history (paths.GenerationTestExport, #932): the
// results the reader tapped on the history, or the newest N of its filter, as photos/ and prompts/prompts.csv. It is a thin layer over
// service/gentest/export, which decides which results go in (the MCP tool export_generation_results uses the same), kept in a package of
// its own for the line limit of the handlers' directory.
package exportscreen

import (
	"errors"
	"mime"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/export"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/exportbar"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the route of the export.
type Handlers struct {
	Svc *gentest.Service
}

// Register adds the route to the engine.
func Register(engine *gin.Engine, svc *gentest.Service) {
	engine.GET(paths.GenerationTestExport(), Handlers{Svc: svc}.Export)
}

// selection reads the query: "id" (repeated), "latest", "batch", "model", "media", "favorite=1" and "commercial".
func selection(c *gin.Context) (export.Selection, error) {
	sel := export.Selection{
		ResultIDs: c.QueryArray("id"), BatchID: c.Query("batch"), FavoriteOnly: c.Query("favorite") == "1",
		Media: c.Query("media"), Commercial: c.Query("commercial"),
	}
	if m := c.Query("model"); m != "" {
		sel.Models = []string{m}
	}
	if v := c.Query("latest"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return sel, gterr.Invalid("件数は数字で指定してください")
		}
		sel.Latest = n
	}
	return sel, nil
}

// Export answers GET /generation-test/export with the ZIP, streamed (the files are copied from the disk one by one).
//
// The bar of the history asks through htmx first (HX-Request): a choice that cannot be exported is then answered with a fragment that
// says why, drawn beside the buttons, and one that can is answered with HX-Redirect to the very same URL, which the browser (or the
// desktop window) loads as a download without leaving the page. A plain request (no script, a copied link) that cannot be exported is
// answered as plain text with the reason, in place of the download.
func (h Handlers) Export(c *gin.Context) {
	sel, err := selection(c)
	var arc export.Archive
	if err == nil {
		arc, err = export.New(h.Svc).Archive(c.Request.Context(), sel)
	}
	asked := c.GetHeader("HX-Request") == "true"
	switch {
	case err != nil:
		refuse(c, err, asked)
		return
	case asked:
		c.Header("HX-Redirect", c.Request.URL.RequestURI())
		c.Status(http.StatusNoContent)
		return
	}
	c.Header("Content-Type", export.ContentType)
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": arc.Name}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	if err := arc.Write(c.Writer); err != nil && c.Request.Context().Err() == nil {
		_ = c.Error(err) // the status is sent already; the broken stream shows as a ZIP that does not open
	}
}

// refuse answers a selection that cannot be exported: the reason as a fragment for htmx (the bar draws it, swapped in although it is an
// error status: main.ts), as plain text otherwise.
func refuse(c *gin.Context, err error, asked bool) {
	var invalid *gterr.InvalidError
	status, msg := http.StatusInternalServerError, "処理できませんでした。mam のログを確認してください"
	switch {
	case errors.As(err, &invalid):
		status, msg = http.StatusUnprocessableEntity, invalid.Message
	case errors.Is(err, gterr.ErrNotFound):
		status, msg = http.StatusNotFound, "対象が見つかりません"
	default:
		_ = c.Error(err)
	}
	if !asked {
		c.String(status, msg)
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	_ = exportbar.Problem(msg).Render(c.Request.Context(), c.Writer)
}
