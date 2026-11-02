package gentest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/rolenotice"
)

// roleNotes are the models of the estimate that do not take the role of a reference and so use the picture as a whole (the warning
// role_unsupported, with the roles it is about), one per model, in the order of the models; none when every model takes what it is given.
func roleNotes(est gentest.Estimate) []rolenotice.Note {
	label := map[string]string{}
	for _, l := range est.Breakdown {
		label[l.Model] = l.Label
	}
	var notes []rolenotice.Note
	for _, w := range est.Warnings {
		if w.Code != gentest.WarnRoleUnsupported || len(w.Roles) == 0 {
			continue
		}
		n := rolenotice.Note{Model: label[w.Model]}
		if n.Model == "" {
			n.Model = w.Model
		}
		for _, r := range w.Roles {
			n.Roles = append(n.Roles, roleNames[r])
		}
		notes = append(notes, n)
	}
	return notes
}

// RunNotice answers POST /generation-test/run-notice: the run button of a confirmation whose estimate has a model that does not take
// the role of a reference. The body of the modal says what each model does with the references (the same estimate, read again from
// the confirmation's form) and carries that form, so that "このまま実行" runs the batch the user confirmed; "戻る" runs nothing.
func (h Handlers) RunNotice(c *gin.Context) {
	f, err := readForm(c, false)
	var est gentest.Estimate
	if err == nil {
		var req gentest.Request
		if req, err = f.request(); err == nil {
			est, err = h.Svc.Estimate(c.Request.Context(), req)
		}
	}
	if err != nil {
		status, msg := refuse(c, err)
		gtshare.Render(c, status, modal.Notice("実行できません", msg))
		return
	}
	form := c.Request.Form
	var fields []rolenotice.Field
	for _, h := range hidden(f, est.Request, form.Get(view.FieldEstimateID), form.Get(view.FieldTotal)) {
		fields = append(fields, rolenotice.Field{Name: h.Name, Value: h.Value})
	}
	gtshare.Render(c, http.StatusOK, rolenotice.Frame(roleNotes(est), fields))
}
