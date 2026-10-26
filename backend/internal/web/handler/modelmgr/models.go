package modelmgr

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

// Models answers GET /model-manager/models, which the list polls while an install runs. The jobs it asks about
// (query jobs, comma separated) are reported once they ended; the choices of the trial form are drawn again then.
func (h Handlers) Models(c *gin.Context) {
	ids := splitIDs(c.Query("jobs"))
	cat := catalogData(c.Request.Context(), h.Svc, ids)
	if !cat.Polling() && len(ids) > 0 {
		trial := h.trialPanel(c.Request.Context(), view.TrialForm{}, "", "")
		trial.OOB = true
		render(c, http.StatusOK, view.CatalogWithTrial(cat, trial))
		return
	}
	render(c, http.StatusOK, view.CatalogList(cat))
}

// Consent answers GET /model-manager/models/:id/consent with the license dialog (what the install button of a model that
// needs an acceptance opens). A model that needs none is 404.
func (h Handlers) Consent(c *gin.Context) {
	m, err := h.Svc.Model(c.Param("id"))
	if err == nil && !m.ConsentRequired {
		err = modelmanager.ErrNotFound
	}
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, view.ConsentDialog(view.Consent{Name: "モデル", Error: msg}))
		return
	}
	render(c, http.StatusOK, view.ConsentDialog(consentOf(m, "")))
}

func consentOf(m modelmanager.ModelView, errMsg string) view.Consent {
	return view.Consent{ModelID: m.ID, Name: m.Name, License: licenseOf(m.License), AdultNotice: m.AdultNotice, Sources: sourcesOf(m), Error: errMsg}
}

// InstallModel starts the install of a model as a job (POST /model-manager/models/:id/install). A model whose license
// needs an acceptance takes it from the fields license_id, license_version and agree=1; the service refuses without a
// record of it (409), so no request can skip the consent. The answer is the list with the job's progress.
func (h Handlers) InstallModel(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")
	var accept *modelmanager.LicenseRef
	if c.PostForm(view.FieldAgree) == "1" {
		accept = &modelmanager.LicenseRef{ID: c.PostForm(view.FieldLicenseID), Version: c.PostForm(view.FieldLicenseV)}
	}
	ref, err := h.Svc.InstallModel(ctx, id, accept)
	if err != nil {
		status, msg := refuse(c, err)
		if m, merr := h.Svc.Model(id); merr == nil && m.ConsentRequired && !m.Accepted {
			// the dialog stays (or opens) with the reason, in the place of the dialog
			c.Header("HX-Retarget", "#"+view.DialogID)
			c.Header("HX-Reswap", "innerHTML")
			render(c, status, view.ConsentDialog(consentOf(m, msg)))
			return
		}
		cat := catalogData(ctx, h.Svc, nil)
		cat.Error = msg
		render(c, status, view.CatalogList(cat))
		return
	}
	cat := catalogData(ctx, h.Svc, []string{ref.JobID})
	cat.Notice = "導入を始めました。この画面を閉じても続きます"
	if ref.Existing {
		cat.Notice = "このモデルは、すでに導入中です"
	}
	render(c, http.StatusAccepted, view.CatalogWithDialogClear(cat))
}

// DeleteModel removes an installed model (DELETE /model-manager/models/:id). A model that an image purpose or a Quality
// Policy uses is 422 with where; a running install is 409; files shared with another installed model stay.
func (h Handlers) DeleteModel(c *gin.Context) {
	ctx := c.Request.Context()
	err := h.Svc.DeleteModel(ctx, c.Param("id"))
	cat := catalogData(ctx, h.Svc, nil)
	if err != nil {
		status, msg := refuse(c, err)
		cat.Error = msg
		render(c, status, view.CatalogList(cat))
		return
	}
	cat.Notice = "モデルを削除しました。他のモデルと共有しているファイルは、使うモデルが残る間は残ります"
	trial := h.trialPanel(ctx, view.TrialForm{}, "", "")
	trial.OOB = true
	render(c, http.StatusOK, view.CatalogWithTrial(cat, trial))
}

// splitIDs is the job ids of a comma separated query value.
func splitIDs(q string) []string {
	var ids []string
	for _, id := range strings.Split(q, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
