// Package ollama holds the handlers of the section "Ollama のモデル" of the model manager screen (#1105; spec/api/endpoints-model-manager.md): the state of the Ollama that mam manages and
// the cards of the three models of the catalog, their fetch, cancel (the job route of the screen) and delete. It is a package of its own because the handler/modelmgr package is at its size limit.
// Every action calls ollamapull.Service, which enforces the rules (a name of the catalog only, the machine, the free space, the Agents that use the model); the screen only asks.
package ollama

import (
	"errors"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/localllm"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	card "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/ollamacard"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the routes of the section.
type Handlers struct {
	svc *ollamapull.Service
	// llm is the server the calls of the provider "local" go to (nil: no such setting). A pull or a delete changes what the line "使うサーバー" says (the models the Ollama can offer),
	// so its answers carry that line out of band.
	llm *llmlocal.Service
}

// Register adds the routes of the section to the engine. llm may be nil.
func Register(engine *gin.Engine, svc *ollamapull.Service, llm *llmlocal.Service) {
	h := Handlers{svc: svc, llm: llm}
	engine.GET(paths.ModelManagerPart("ollama"), h.Section)
	engine.GET(paths.ModelManagerPart("ollama", "models", "detail"), h.Detail)
	engine.POST(paths.ModelManagerPart("ollama", "models", "pull"), h.Pull)
	engine.DELETE(paths.ModelManagerPart("ollama", "models"), h.Delete)
}

// Cancel stops the running pull of a job (the cancel button of a card, POST /model-manager/jobs/:job/cancel, which the handler of the screen routes here for a job whose target is a
// model of Ollama) and answers with the section.
func Cancel(c *gin.Context, svc *ollamapull.Service, llm *llmlocal.Service, jobID string) {
	h := Handlers{svc: svc, llm: llm}
	if err := svc.Cancel(jobID); err != nil {
		h.answer(c, http.StatusConflict, func(s *card.Section) {
			s.Error = "中断できる取得ではありません。画面を読み込み直してください"
		}, jobID)
		return
	}
	h.answer(c, http.StatusOK, func(s *card.Section) { s.Notice = "取得を中断しています" }, jobID)
}

// refuse is the status and message of an error of the service: 422 for an input that is refused, 409 when the state does not allow it, 404 for what is not there. A failure with a
// sentence of its own (Ollama did not start, did not answer) is a 502 with that sentence; any other is a generic 500 with the cause in the log.
func refuse(c *gin.Context, err error) (status int, msg string) {
	kind, msg := mmview.RefuseRuntime(err)
	switch kind {
	case mmview.RefusalInvalid:
		return http.StatusUnprocessableEntity, msg
	case mmview.RefusalConflict:
		return http.StatusConflict, msg
	case mmview.RefusalNotFound:
		return http.StatusNotFound, msg
	}
	var worded interface{ UserMessage() string }
	if errors.As(err, &worded) {
		return http.StatusBadGateway, worded.UserMessage()
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "処理できませんでした。mam のログを確認してください"
}

// answer draws the section (with the line "使うサーバー" out of band) and edits it first. jobs are the pulls it reports on.
func (h Handlers) answer(c *gin.Context, status int, edit func(*card.Section), jobs ...string) {
	ctx := c.Request.Context()
	s := sectionData(ctx, h.svc, jobs)
	if edit != nil {
		edit(&s)
	}
	out := card.OllamaSection(s)
	if h.llm != nil {
		out = templ.Join(out, localllm.StateOOB(ctx, h.llm))
	}
	render(c, status, out)
}

// Section answers GET /model-manager/ollama with the section read again: the refresh button, and the poll while a pull runs (?jobs= are the pulls it reports on: one that ended says how).
func (h Handlers) Section(c *gin.Context) {
	h.answer(c, http.StatusOK, nil, jobIDs(c.Query("jobs"))...)
}

// Detail answers GET /model-manager/ollama/models/detail?name= with the modal of a model: its layers, the SHA-256 of the weights and what the check of the manifest says. A name that
// is not in the catalog is the reason in the modal (404).
func (h Handlers) Detail(c *gin.Context) {
	for _, m := range h.svc.Catalog() {
		if m.Name == c.Query("name") {
			render(c, http.StatusOK, card.ModelDetail(detailData(h.svc, m)))
			return
		}
	}
	render(c, http.StatusNotFound, modal.Notice("Ollama のモデル", "このモデルは一覧にありません。画面を読み込み直してください"))
}

// Pull starts the fetch of a model as a job (POST /model-manager/ollama/models/pull, field name) and answers with the section showing its progress. Only a name of the catalog is
// accepted (422 for any other, a cloud model and another namespace included, before any request is made to Ollama); an Ollama that is not installed or cannot run here, an install that is
// running and a full disk are 409 with the reason.
func (h Handlers) Pull(c *gin.Context) {
	ref, err := h.svc.Pull(c.Request.Context(), c.PostForm("name"))
	if err != nil {
		status, msg := refuse(c, err)
		h.answer(c, status, func(s *card.Section) { s.Error = msg })
		return
	}
	h.answer(c, http.StatusAccepted, func(s *card.Section) { s.Notice = "取得を始めました。この画面を閉じても続きます" }, ref.JobID)
}

// Delete removes a fetched model (DELETE /model-manager/ollama/models?name=). A model an Agent uses is 422 (it names the Agents); one that is being pulled or not fetched is 409.
func (h Handlers) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Query("name")); err != nil {
		status, msg := refuse(c, err)
		h.answer(c, status, func(s *card.Section) { s.Error = msg })
		return
	}
	h.answer(c, http.StatusOK, func(s *card.Section) { s.Notice = "モデルを削除しました" })
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
