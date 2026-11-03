package gentest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/inputnotice"
)

// inputCheckOf is the notice of a check of the input images: the stop as the common error, and the warnings.
func inputCheckOf(ic gentest.InputCheck) inputnotice.Check {
	var out inputnotice.Check
	if ic.Block != nil {
		v := ic.Block.View(apperr.LangJa)
		out.Block = &v
	}
	for _, w := range ic.Warnings {
		out.Warnings = append(out.Warnings, w.Message)
	}
	return out
}

// InputCheck answers POST /generation-test/input-check (the form, without its files): what the ticked models do with the input images attached now.
// It is the check the estimate and the MCP tools make (Service.CheckInputs), run as the form is filled in, so that the user sees "この画像は使われません" and
// "最初のフレームが必要です" when they tick the model or change a role, not only after pressing the button. Nothing is staged and nothing is priced.
func (h Handlers) InputCheck(c *gin.Context) {
	f, err := readForm(c, false)
	if err != nil {
		status, _ := refuse(c, err)
		c.AbortWithStatus(status)
		return
	}
	gtshare.Render(c, http.StatusOK, inputnotice.CheckBox(inputCheckOf(h.Svc.CheckInputs(gentest.Request{Media: f.media, Models: f.models, References: f.refs}))))
}
