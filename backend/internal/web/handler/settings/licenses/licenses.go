// Package licenses is the handlers of the "第三者のライセンス" category of the settings screen
// (spec/components/settings-screen-licenses.md): the data of the page body and GET /settings/licenses/:id, the text of one entry.
package licenses

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/licenses"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/licenses"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// SectionData is the groups of the catalog, each entry with the path its text is read from.
func SectionData(c *licenses.Catalog) view.Data {
	var d view.Data
	for _, g := range c.Groups() {
		vg := view.Group{Name: g.Name}
		for _, s := range g.Sections {
			vg.Items = append(vg.Items, view.Item{ID: s.ID, Title: s.Title, TextURL: paths.LicenseText(s.ID)})
		}
		d.Groups = append(d.Groups, vg)
	}
	return d
}

// Text answers GET /settings/licenses/:id with the text of the entry: a fragment for the entry that asked for it (an HTMX
// request). A request that is not HTMX (the address typed in, a reload) gets page, the page of the category. An id that names
// no entry is 404. The text does not change while mam runs.
func Text(c *licenses.Catalog, page gin.HandlerFunc) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !settingshttp.IsHTMX(ctx) {
			page(ctx)
			return
		}
		s, ok := c.Find(ctx.Param("id"))
		if !ok {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		settingshttp.Render(ctx, http.StatusOK, view.Text(s.Title, s.Body))
	}
}
