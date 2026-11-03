package gentest

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

// RefRow answers GET /generation-test/ref-row: the row of one chosen input image ("?ref=<source>:<project>:<id>").
func (h Handlers) RefRow(c *gin.Context) {
	r, ok := refform.Parse(c.Query(view.FieldRef))
	if !ok {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	ctx, media := c.Request.Context(), c.Query(view.FieldMedia)
	role := gentest.RoleReference
	if r.Source == gentest.FromPoseLibrary { // a pose picture is a pose until the user says otherwise
		role = gentest.RolePose
	}
	if media == gentest.MediaVideo { // the first frame while a chosen model needs one and none of the pictures in the form is one (#1009)
		var have []gentest.RefInput
		for key, vals := range c.Request.URL.Query() {
			if strings.HasPrefix(key, view.FieldRolePrefix) && len(vals) > 0 {
				have = append(have, gentest.RefInput{Role: vals[0]})
			}
		}
		role = h.Svc.DefaultInputRole(media, c.QueryArray(view.FieldModel), have)
	}
	row, ok := h.refRow(ctx, refform.Input(r, role, nil))
	if !ok {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	var ui view.RefUI
	if media != gentest.MediaVideo { // the controls follow the models that are ticked now
		ui = view.RefUIOf(choicesOf(h.Svc.Models(ctx, gentest.MediaImage), c.QueryArray(view.FieldModel)))
	}
	gtshare.Render(c, http.StatusOK, view.RefRow(row, media, ui))
}
