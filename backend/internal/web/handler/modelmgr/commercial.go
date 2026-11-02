package modelmgr

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

// CommercialFilterID is the id of the filter of the model list; the list is view.ModelsID.
const CommercialFilterID = "mm-commercial-filter"

// commercialData is the setting "商用OKのモデルだけを使う" with the filter of the model list, checked at the setting's answer.
func commercialData(svc *modelmanager.Service) view.CommercialPanel {
	return view.CommercialPanel{
		Only:   svc.CommercialOnly(),
		Filter: commercial.NewFilter(CommercialFilterID, "#"+view.ModelsID, svc.DefaultFilter()),
	}
}

// SetCommercial turns "商用OKのモデルだけを使う" on or off (PUT /model-manager/commercial; field on = 1 or 0). The answer is the card: the filter
// is drawn at the new default, which the filter's script applies to the list. The trial choices are drawn again out of band, so that their
// filter starts at the new default too.
func (h Handlers) SetCommercial(c *gin.Context) {
	ctx := c.Request.Context()
	on := c.PostForm(view.FieldOn) == "1"
	if err := h.Svc.SetCommercialOnly(on); err != nil {
		status, msg := refuse(c, err)
		p := commercialData(h.Svc)
		p.Error = msg
		render(c, status, view.CommercialCard(p))
		return
	}
	p := commercialData(h.Svc)
	p.Notice = "モデルの一覧は、すべてのモデルから始めます"
	if on {
		p.Notice = "モデルの一覧は、商用OKのモデルだけから始めます（画面のフィルターで広げられます）"
	}
	trial := h.trialPanel(ctx, view.TrialForm{}, "", "")
	trial.OOB = true
	render(c, http.StatusOK, view.CommercialWithTrial(p, trial))
}
