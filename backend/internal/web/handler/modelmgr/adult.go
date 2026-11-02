package modelmgr

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

// adultData is the setting "成人向けモデルを表示する".
func adultData(svc *modelmanager.Service) view.Adult {
	st := svc.AdultContent()
	a := view.Adult{Enabled: st.Enabled, Confirmed: st.Confirmed}
	if st.ConfirmedAt != nil {
		a.ConfirmedAt = st.ConfirmedAt.Local().Format("2006-01-02 15:04")
	}
	return a
}

// AdultConfirm answers GET /model-manager/adult/confirm with the dialog that asks the age confirmation (what the first
// turning on of the setting opens). The dialog is only how the user gives it: the server decides in SetAdult.
func (h Handlers) AdultConfirm(c *gin.Context) {
	st := h.Svc.AdultContent()
	render(c, http.StatusOK, view.AdultDialog(view.AdultConfirm{Notice: st.Notice, NoticeVersion: st.NoticeVersion}))
}

// SetAdult turns "show adult models" on or off (PUT /model-manager/adult; fields on = 1 or 0, and for the first turning on
// notice_version with agree=1: the confirmation of the age and of the notice the dialog showed). Turning it on without a
// confirmation on record is 409, one that does not say it is 18 or over is 422: no request can skip the confirmation.
// The answer is the card, with the model list and the trial choices drawn again out of band.
func (h Handlers) SetAdult(c *gin.Context) {
	ctx := c.Request.Context()
	on := c.PostForm(view.FieldOn) == "1"
	var confirmation *modelmanager.AdultConfirmation
	version, fromDialog := c.GetPostForm(view.FieldNoticeV)
	if fromDialog {
		confirmation = &modelmanager.AdultConfirmation{Adult: c.PostForm(view.FieldAgree) == "1", NoticeVersion: version}
	}
	if err := h.Svc.SetAdultContent(on, confirmation); err != nil {
		status, msg := refuse(c, err)
		if fromDialog {
			st := h.Svc.AdultContent()
			c.Header("HX-Retarget", "#"+view.DialogID)
			c.Header("HX-Reswap", "innerHTML")
			render(c, status, view.AdultDialog(view.AdultConfirm{Notice: st.Notice, NoticeVersion: st.NoticeVersion, Error: msg}))
			return
		}
		a := adultData(h.Svc)
		a.Error = msg
		render(c, status, view.AdultCard(a))
		return
	}
	a := adultData(h.Svc)
	a.Notice = "成人向けモデルを隠しました（確認の記録は残ります）"
	if on {
		a.Notice = "成人向けモデルを表示します。モデルごとのライセンスへの同意は、導入のときに取ります"
	}
	cat := catalogData(ctx, h.Svc, nil)
	cat.OOB = true
	trial := h.trialPanel(ctx, view.TrialForm{}, "", "")
	trial.OOB = true
	render(c, http.StatusOK, view.AdultWithCatalog(a, cat, trial))
}

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
