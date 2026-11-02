package gentest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/tierform"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentesttier"
)

// EasyPick answers GET /generation-test/easy-pick (Issue #977): the part of the easy tier that follows the choice of what to make ("?intent=",
// "?ref=" for how many input images the form holds, "?quality=" for the one that is lit): the model that choice picks, why there is none, and the
// quality buttons of that model. It is swapped on its own, so that a file chosen in the browser stays when the choice changes.
func (h Handlers) EasyPick(c *gin.Context) {
	ctx, v := c.Request.Context(), c.Request.URL.Query()
	st := tierform.Read(v)
	st.Tier = gentesttier.TierEasy
	e, picked := tierform.Pick(ctx, h.Svc, st, len(refform.Refs(v, gentest.MediaImage)))
	var chosen []view.ModelChoice
	for _, m := range choicesOf(h.Svc.Models(ctx, gentest.MediaImage), picked) {
		if m.Selected && m.Reason == "" {
			chosen = append(chosen, m)
		}
	}
	gtshare.Render(c, http.StatusOK, gentesttier.EasyPick(e, view.EasyCard(chosen)))
}
