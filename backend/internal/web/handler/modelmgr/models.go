package modelmgr

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/nodegate"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/catalogfilter"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/licenseview"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/modeldetail"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/oob"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/reason"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Models answers GET /model-manager/models, which the list polls while an install runs. The jobs it asks about
// (query jobs, comma separated) are reported once they ended.
func (h Handlers) Models(c *gin.Context) {
	ids := splitIDs(c.Query("jobs"))
	cat := catalogData(c, h.Svc, ids)
	if cond, _ := catalogfilter.Screen().Read(c); true {
		catalogfilter.Screen().Remember(c, cond) // a request of the filter bar states the condition: the screen opens with it next time
	}
	if !cat.Polling() && len(ids) > 0 {
		render(c, http.StatusOK, view.CatalogList(cat))
		return
	}
	render(c, http.StatusOK, view.CatalogWithBar(cat))
}

// ModelDetail answers GET /model-manager/models/:id/detail with the modal of the card: the license, the sources, the memory and the
// files with their SHA-256 (the list shows none of it). A model that is not there, or an adult one while the age gate is closed, is the
// reason in the modal (404 / 409), so that a direct request does not name what the list hides.
func (h Handlers) ModelDetail(c *gin.Context) {
	ctx := c.Request.Context()
	m, err := h.Svc.Model(c.Param("id"))
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, modal.Notice("モデル", msg))
		return
	}
	hasToken, tokenErr := h.Svc.HasHuggingFaceToken()
	md := modelData(ctx, h.Svc, mmview.EntryOf(h.Svc, m), h.Svc.Engine().FreeBytes, mmview.GPUVRAM(ctx, h.Svc), tokenErr == nil && !hasToken)
	render(c, http.StatusOK, modeldetail.Detail(md))
}

// ModelReason answers GET /model-manager/models/:id/blocked with the popup of why the model cannot be installed now and what to do (#1118): the custom
// nodes its workflows need that the engine does not load, each with what is wrong with it, and the link to the screen of the nodes. It only explains: it
// starts nothing, and the install of the model stays refused (InstallModel) whatever is asked here. A model that nothing is in the way of is 404, so the
// popup never invents a reason.
func (h Handlers) ModelReason(c *gin.Context) {
	m, err := h.Svc.Model(c.Param("id"))
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, modal.Notice("モデル", msg))
		return
	}
	problems := nodegate.Problems(h.Svc.Nodes, m.RequiresNodes)
	if len(problems) == 0 || m.State == "installed" {
		render(c, http.StatusNotFound, modal.Notice(m.Name, "このモデルには、導入できない理由がありません"))
		return
	}
	render(c, http.StatusOK, reason.Body(nodesPopup(m.Name, problems)))
}

// nodesPopup is the popup of a model that waits for custom nodes: a node mam does not install at all says why in the words of the allowlist, and the
// others are to be installed on the screen of the nodes. Nothing here says a dependency is resolved or that mam will install one by itself.
func nodesPopup(model string, problems []nodegate.Problem) reason.Popup {
	p := reason.Popup{
		Title: "「" + model + "」を導入できない理由と対処", Lead: "このモデルを使うには、追加ノードが要ります。",
		Link: &reason.Link{Href: paths.ModelManagerPart(mmcat.CustomNodes) + "#" + view.NodesID, Label: "追加ノードの画面へ"},
	}
	p.Remedy = "「追加ノード」の画面で、足りないノードを導入してください。導入が終わると、このモデルを導入できます。"
	for _, pr := range problems {
		text := "導入されていません。"
		if pr.Blocked != "" {
			text = "今の mam では導入できません。" + pr.Blocked
			p.Remedy = "このモデルが必要とする追加ノードは、今の mam では導入できません。mam は Python の依存を自動で入れません。導入できるようになるまで、このモデルは導入できません。ノードごとの状態は「追加ノード」の画面で確かめられます。"
		}
		p.Items = append(p.Items, reason.Item{Name: "追加ノード「" + pr.Name + "」", Text: text})
	}
	return p
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
	render(c, http.StatusOK, view.ConsentDialog(consentOf(c, h.Svc, m, "")))
}

func consentOf(c *gin.Context, svc *modelmanager.Service, m modelmanager.ModelView, errMsg string) view.Consent {
	return view.Consent{ModelID: m.ID, Name: m.Name, License: licenseview.Of(m.License), AdultNotice: m.AdultNotice, Sources: sourcesOf(m), PlatformWarning: mmview.PlatformWarning(c.Request.Context(), svc, m), Error: errMsg}
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
			render(c, status, view.ConsentDialog(consentOf(c, h.Svc, m, msg)))
			return
		}
		cat := catalogData(c, h.Svc, nil)
		cat.Error = msg
		render(c, status, view.CatalogList(cat))
		return
	}
	cat := catalogData(c, h.Svc, []string{ref.JobID})
	cat.Notice = "導入を始めました。この画面を閉じても続きます"
	if ref.Existing {
		cat.Notice = "このモデルは、すでに導入中です"
	}
	render(c, http.StatusAccepted, oob.CatalogWithDialogClear(cat))
}

// DeleteModel removes an installed model (DELETE /model-manager/models/:id). A model that an image purpose or a Quality
// Policy uses is 422 with where; a running install is 409; files shared with another installed model stay.
func (h Handlers) DeleteModel(c *gin.Context) {
	ctx := c.Request.Context()
	err := h.Svc.DeleteModel(ctx, c.Param("id"))
	cat := catalogData(c, h.Svc, nil)
	if err != nil {
		status, msg := refuse(c, err)
		cat.Error = msg
		render(c, status, view.CatalogList(cat))
		return
	}
	cat.Notice = "モデルを削除しました。他のモデルと共有しているファイルは、使うモデルが残る間は残ります"
	render(c, http.StatusOK, view.CatalogList(cat))
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
