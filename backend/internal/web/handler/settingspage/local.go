package settingspage

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	localhandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/local"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/settings/local"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// LocalEndpoint is the handler of the page of the endpoint :name of the local models category
// (/settings/local-models/endpoints/:name, spec/components/settings-screen-categories.md §5): its summary and its models.
// It is also what the forms of a model answer a request that is not HTMX with. An endpoint that is not there is 404.
func (s Services) LocalEndpoint() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.LocalModels == nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		d := localhandler.SectionData(s.LocalModels)
		name := c.Param("name")
		i := slices.IndexFunc(d.Endpoints, func(e local.Endpoint) bool { return e.Name == name })
		if i < 0 || d.Endpoints[i].NoPage {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "no-store")
		own := paths.LocalEndpoint(name)
		s.render(c, paths.SettingsLocalModels, "設定: ローカル・代替モデル: "+name, canonicalOf(c, own), pages.SettingsLocalEndpointBody(d, d.Endpoints[i]))
	}
}
