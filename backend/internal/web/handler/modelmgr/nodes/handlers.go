// Package nodes holds the handlers of the section "追加ノード" of the model manager screen (spec/api/endpoints-model-manager.md): the cards
// of the custom nodes of the allowlist, their install (with the consent to the license and the security notice) and removal. It is a package of
// its own because the handler/modelmgr package is at its size limit. Every action calls modelnodes.Service, which enforces the rules (the
// consent, the machine, the free space); the screen only asks.
package nodes

import (
	"errors"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	card "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/nodecard"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/reason"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the routes of the section.
type Handlers struct {
	svc *modelnodes.Service
}

// Register adds the routes of the section to the engine.
func Register(engine *gin.Engine, svc *modelnodes.Service) {
	h := Handlers{svc: svc}
	engine.GET(paths.ModelManagerPart("nodes"), h.Section)
	engine.GET(paths.ModelManagerPart("nodes", ":id"), h.Node)
	engine.GET(paths.ModelManagerPart("nodes", ":id", "detail"), h.Detail)
	engine.GET(paths.ModelManagerPart("nodes", ":id", "blocked"), h.Reason)
	engine.GET(paths.ModelManagerPart("nodes", ":id", "consent"), h.Consent)
	engine.POST(paths.ModelManagerPart("nodes", ":id", "install"), h.Install)
	engine.POST(paths.ModelManagerPart("nodes", ":id", "cancel"), h.Cancel)
	engine.DELETE(paths.ModelManagerPart("nodes", ":id"), h.Remove)
}

// refuse is the status and message of an error of the node service: the same kinds as the models (422, 409, 404); a fault is a generic 500
// with the cause in the log.
func refuse(c *gin.Context, err error) (status int, msg string) {
	if errors.Is(err, modelmanager.ErrNotInstalled) {
		return http.StatusConflict, "導入されていません"
	}
	kind, msg := mmview.Refuse(err)
	switch kind {
	case mmview.RefusalInvalid:
		return http.StatusUnprocessableEntity, msg
	case mmview.RefusalConflict:
		return http.StatusConflict, msg
	case mmview.RefusalNotFound:
		return http.StatusNotFound, msg
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "処理できませんでした。mam のログを確認してください"
}

// nodeCard answers an action of a node with its card. A node that is not there is a 404 with the section's message.
func (h Handlers) nodeCard(c *gin.Context, status int, id, job string, edit func(*card.Node)) {
	n, err := nodeData(c.Request.Context(), h.svc, id, job)
	if err != nil {
		st, msg := refuse(c, err)
		s := sectionData(c.Request.Context(), h.svc)
		s.Error = msg
		render(c, st, card.NodesSection(s))
		return
	}
	if edit != nil {
		edit(&n)
	}
	render(c, status, card.NodeCard(n))
}

// failed answers a refused action: the card with the reason (409 / 404 / 422).
func (h Handlers) failed(c *gin.Context, id string, err error) {
	status, msg := refuse(c, err)
	h.nodeCard(c, status, id, "", func(n *card.Node) { n.Error = msg })
}

// Section answers GET /model-manager/nodes with every card read again: the refresh button of the section (#962).
func (h Handlers) Section(c *gin.Context) {
	render(c, http.StatusOK, card.NodesSection(sectionData(c.Request.Context(), h.svc)))
}

// Node answers GET /model-manager/nodes/:id, which the card polls while an install is in progress. job is the install it reports on: once
// that ended, the card says how.
func (h Handlers) Node(c *gin.Context) {
	h.nodeCard(c, http.StatusOK, c.Param("id"), c.Query("job"), nil)
}

// Reason answers GET /model-manager/nodes/:id/blocked with the popup of why mam does not install the node (install_blocked, in the words of the allowlist) and what
// that means for the models that need it (#1118). It only explains: the install of the node stays refused by the service whatever is asked here. A node mam can
// install has no such reason, so it is 404.
func (h Handlers) Reason(c *gin.Context) {
	n, err := nodeData(c.Request.Context(), h.svc, c.Param("id"), "")
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, modal.Notice("追加ノード", msg))
		return
	}
	if n.Blocked == "" {
		render(c, http.StatusNotFound, modal.Notice(n.Name, "このノードには、導入できない理由がありません"))
		return
	}
	render(c, http.StatusOK, reason.Body(reason.Popup{
		Title: "「" + n.Name + "」を導入できない理由と対処", Lead: "このノードは、今の mam では導入できません。",
		Items:  []reason.Item{{Text: n.Blocked}},
		Remedy: "mam は Python の依存を自動で入れません。導入できるようになるまで、このノードと、このノードを必要とするモデルは導入できません。",
	}))
}

// Detail answers GET /model-manager/nodes/:id/detail with the modal of the card: the pinned commit and SHA-256, the license and the answer
// for commercial use, the dependencies and the review (the card shows none of it).
func (h Handlers) Detail(c *gin.Context) {
	n, err := nodeData(c.Request.Context(), h.svc, c.Param("id"), "")
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, modal.Notice("追加ノード", msg))
		return
	}
	render(c, http.StatusOK, card.NodeDetail(n))
}

// Consent answers GET /model-manager/nodes/:id/consent with the dialog of the install button: the security notice and the license, with
// the form that accepts them.
func (h Handlers) Consent(c *gin.Context) {
	n, err := nodeData(c.Request.Context(), h.svc, c.Param("id"), "")
	if err != nil {
		status, msg := refuse(c, err)
		render(c, status, card.ConsentDialog(card.Node{Name: "追加ノード", Message: view.Message{Error: msg}}))
		return
	}
	render(c, http.StatusOK, card.ConsentDialog(n))
}

// Install starts the install of a node as a job (POST /model-manager/nodes/:id/install). The acceptance comes from the fields license_id,
// license_version, notice_version and agree=1; the service refuses without a record of it (409/422), so no request can skip the consent.
// The answer is the card with its progress, or the dialog again with the reason when the consent is what is missing.
func (h Handlers) Install(c *gin.Context) {
	id := c.Param("id")
	var accept *modelnodes.LicenseRef
	if c.PostForm(view.FieldAgree) == "1" {
		accept = &modelnodes.LicenseRef{ID: c.PostForm(view.FieldLicenseID), Version: c.PostForm(view.FieldLicenseV), NoticeVersion: c.PostForm(view.FieldNoticeV)}
	}
	ref, err := h.svc.Install(c.Request.Context(), id, accept)
	if err != nil {
		status, msg := refuse(c, err)
		if n, nerr := nodeData(c.Request.Context(), h.svc, id, ""); nerr == nil && !n.Accepted && n.Blocked == "" {
			// the dialog stays (or opens) with the reason, in the place of the dialog
			c.Header("HX-Retarget", "#"+card.DialogID)
			c.Header("HX-Reswap", "innerHTML")
			n.Error = msg
			render(c, status, card.ConsentDialog(n))
			return
		}
		h.failed(c, id, err)
		return
	}
	h.nodeCard(c, http.StatusAccepted, id, ref.JobID, func(n *card.Node) {
		n.Error, n.Notice = "", n.Name+" の導入を始めました。この画面を閉じても続きます"
		if ref.Existing {
			n.Notice = n.Name + " は、すでに導入中です"
		}
	})
}

// Cancel stops the running install of a node (POST /model-manager/nodes/:id/cancel, field job) and answers with the card. The job ends as
// failed with a message that says it was stopped; installing again continues the download.
func (h Handlers) Cancel(c *gin.Context) {
	id, job := c.Param("id"), c.PostForm("job")
	if err := h.svc.CancelInstall(job); err != nil {
		h.failed(c, id, err)
		return
	}
	h.nodeCard(c, http.StatusOK, id, job, func(n *card.Node) { n.Notice = "導入を中断しています" })
}

// Remove deletes the node (DELETE /model-manager/nodes/:id): the engine restarts without it. An install in progress is 409.
func (h Handlers) Remove(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Remove(c.Request.Context(), id); err != nil {
		h.failed(c, id, err)
		return
	}
	h.nodeCard(c, http.StatusOK, id, "", func(n *card.Node) { n.Notice = n.Name + " を削除しました" })
}

func render(c *gin.Context, status int, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}
}
