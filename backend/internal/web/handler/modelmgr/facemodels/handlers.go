// Package facemodels holds the handlers of the section "顔チェック用モデル" of the model manager screen (Issue #1026, spec/api/endpoints-model-manager.md): the card of the
// YuNet and SFace models of the face similarity, their install (the licenses and the source are shown above the button and the box must be ticked: the service
// refuses without it) and removal, and the progress line of an install. It is a package of its own because handler/modelmgr is at its size limit. Every action calls
// facemodels.Service, which enforces the rules; the screen only asks.
package facemodels

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/face"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/facemodels"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	card "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/facecard"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the routes of the section.
type Handlers struct{ svc *facemodels.Service }

// Register adds the routes of the section to the engine.
func Register(engine *gin.Engine, svc *facemodels.Service) {
	h := Handlers{svc: svc}
	engine.GET(paths.ModelManagerPart("face-models"), h.Section)
	engine.POST(paths.ModelManagerPart("face-models", "install"), h.Install)
	engine.GET(paths.ModelManagerPart("face-models", "progress"), h.Progress)
	engine.DELETE(paths.ModelManagerPart("face-models"), h.Remove)
}

// Section is the section of the screen: nil (the section is left out) when the app has no face models service.
func Section(svc *facemodels.Service) templ.Component {
	if svc == nil {
		return nil
	}
	return card.Section(cardOf(svc, view.Message{}))
}

func sizeText(n int64) string { return fetch.HumanBytes(uint64(max(n, 0))) } //nolint:gosec // clamped

var stateText = map[facemodels.State]string{
	facemodels.StateInstalled:  "導入済み",
	facemodels.StateUnverified: "SHA-256 は未確認",
	facemodels.StateMissing:    "未導入",
}

// cardOf reads the state of the models and draws it with the message of the last action.
func cardOf(svc *facemodels.Service, m view.Message) card.Card {
	st := svc.Status()
	c := card.Card{
		Message: m, State: string(st.State), ModelDir: st.ModelDir, Download: sizeText(st.DownloadBytes), Running: svc.Progress().Running,
	}
	if st.RemainBytes < st.DownloadBytes {
		c.Remain = sizeText(st.RemainBytes)
	}
	for _, f := range st.Files {
		c.Files = append(c.Files, card.File{
			Name: f.Name, Title: f.Title, Description: f.Description, Size: sizeText(f.Size), State: string(f.State), StateText: stateText[f.State],
			SourceName: f.SourceName, SourceURL: f.SourceURL,
		})
	}
	for _, l := range st.Licenses {
		c.Licenses = append(c.Licenses, card.License{
			Name: l.Name, Summary: l.Summary, URL: l.URL, Notice: l.Notice, Answer: commercial.View{Level: l.Commercial, LicenseName: l.Name, SourceURL: l.URL, Notice: l.Notice},
		})
	}
	if rec := face.OffRecord(st.Off); rec != nil {
		c.OffReason, c.OffCause, c.OffFix = rec.Skipped, rec.Reason, rec.Fix
	}
	return c
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

// refuse is the status and message of a refused action (an *gterr.InvalidError is 422, a *gterr.ConflictError 409); a fault is a generic 500 with the
// cause in the log.
func refuse(c *gin.Context, err error) (int, string) {
	var (
		invalid  *gterr.InvalidError
		conflict *gterr.ConflictError
	)
	switch {
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, invalid.Message
	case errors.As(err, &conflict):
		return http.StatusConflict, conflict.Message
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "処理できませんでした。mam のログを確認してください"
}

// Section answers GET /model-manager/face-models with the section read again.
func (h Handlers) Section(c *gin.Context) {
	render(c, http.StatusOK, card.Section(cardOf(h.svc, view.Message{})))
}

// Install answers POST /model-manager/face-models/install: the files are downloaded and verified (the box must be ticked: the service refuses without it) and the
// section comes back, with what went wrong when something did. The models work from the next use of the face similarity: nothing restarts.
func (h Handlers) Install(c *gin.Context) {
	accepted := slices.Contains([]string{"on", "1", "true"}, c.PostForm(card.FieldAccept))
	if err := h.svc.Install(c.Request.Context(), accepted, nil); err != nil {
		if c.Request.Context().Err() != nil {
			return // the user left: what was downloaded stays for the next try
		}
		status, msg := refuse(c, err)
		render(c, status, card.Section(cardOf(h.svc, view.Message{Error: msg})))
		return
	}
	render(c, http.StatusOK, card.Section(cardOf(h.svc, view.Message{Notice: installedNotice(h.svc)})))
}

// installedNotice says what the install did, and when the face similarity is still off after it, that the models are not what is missing.
func installedNotice(svc *facemodels.Service) string {
	if st := svc.Status(); st.Off != nil {
		return "顔チェック用モデルを導入しました。ただし顔の類似度はまだオフです。理由は下のとおりです"
	}
	return "顔チェック用モデルを導入しました。再起動は要りません。次の単発生成から、役割「顔だけ」の参照を付けた結果に顔の類似度が付きます"
}

// Remove answers DELETE /model-manager/face-models: the two files are deleted and the section comes back as the offer to install (models that are
// already gone are the same answer; a running install is refused: 409 and its message).
func (h Handlers) Remove(c *gin.Context) {
	if err := h.svc.Remove(); err != nil {
		status, msg := refuse(c, err)
		render(c, status, card.Section(cardOf(h.svc, view.Message{Error: msg})))
		return
	}
	render(c, http.StatusOK, card.Section(cardOf(h.svc, view.Message{Notice: "顔チェック用モデルを削除しました。顔の類似度はオフです"})))
}

// Progress answers GET /model-manager/face-models/progress: how far the install is, as the line the section polls ("" when none runs).
func (h Handlers) Progress(c *gin.Context) {
	p := h.svc.Progress()
	text := ""
	if p.Running && p.Total > 0 {
		text = fmt.Sprintf("ダウンロード中: %s / %s（%d%%）", sizeText(p.Done), sizeText(p.Total), min(100, p.Done*100/p.Total))
	} else if p.Running {
		text = "ダウンロードの準備をしています…"
	}
	render(c, http.StatusOK, card.ProgressLine(p.Running, text))
}
