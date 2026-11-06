// Package localllm holds the handlers of the page "ローカル LLM" of the model manager (spec/components/model-manager-screen.md, "ローカル LLM の統合（#1104）"): the section "使うサーバー"
// (which server the calls of the provider "local" go to; PUT /model-manager/local-llm/use) and the section "別のサーバーにつなぐ" (the address, the optional API key, the unload switch
// and the connection test; PUT /model-manager/local-llm/connection, POST …/connection/test; they were the card of the settings screen). It is a package of its own because the
// handler/modelmgr package is at its size limit. The API key is read from the form, handed to the service and never drawn again.
package localllm

import (
	"errors"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/web/handler/failure"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/llmcard"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

const (
	saved    = "保存しました。次の LLM の呼び出しからすぐ使われます（再起動は要りません）"
	pinned   = "起動オプションで設定ファイルが固定されているため、ベース URL・スイッチ・使うサーバーは変えられません"
	keyStore = "API キーを OS のセキュアストレージに保存できませんでした。キーストアを確認してください"
)

// Handlers answer the routes of the page.
type Handlers struct{ svc *llmlocal.Service }

// Register adds the routes of the page to the engine.
func Register(engine *gin.Engine, svc *llmlocal.Service) {
	h := Handlers{svc: svc}
	engine.GET(paths.ModelManagerLocalLLMUse(), h.Use)
	engine.PUT(paths.ModelManagerLocalLLMUse(), h.SaveUse)
	engine.PUT(paths.ModelManagerLocalLLMConnection(), h.SaveConnection)
	engine.POST(paths.ModelManagerLocalLLMConnectionTest(), h.TestConnection)
}

// Use answers GET /model-manager/local-llm/use with the section "使うサーバー" (part=state: the line of the state alone, which the section polls while a server installs or starts).
func (h Handlers) Use(c *gin.Context) {
	noStore(c)
	u := UseData(c.Request.Context(), h.svc)
	if c.Query("part") == "state" {
		render(c, http.StatusOK, llmcard.UseState(u.State, false))
		return
	}
	render(c, http.StatusOK, llmcard.UseSection(u))
}

// SaveUse saves the server the calls go to (PUT /model-manager/local-llm/use, field use) and answers with the section: 200 with what it is now, 422 for a word that is none of
// llama-cpp / ollama / custom (nothing is saved), 409 for a settings file that a start option pinned or a server of mam that this app does not have. Nothing is installed or started.
func (h Handlers) SaveUse(c *gin.Context) {
	noStore(c)
	use, ok := llmlocal.ParseUse(c.PostForm(llmcard.FieldUse))
	if !ok {
		h.answerUse(c, http.StatusUnprocessableEntity, "", "使うサーバーは「この PC の llama.cpp」「Ollama」「別のサーバー」のどれかです")
		return
	}
	var invalid *llmlocal.InvalidError
	switch err := h.svc.SetUse(use); {
	case err == nil:
		h.answerUse(c, http.StatusOK, saved, "")
	case errors.As(err, &invalid):
		h.answerUse(c, http.StatusUnprocessableEntity, "", invalid.Message)
	case errors.Is(err, llmlocal.ErrPinned):
		h.answerUse(c, http.StatusConflict, "", pinned)
	case errors.Is(err, llmlocal.ErrManagedUnavailable), errors.Is(err, llmlocal.ErrManagedOllamaUnavailable):
		h.answerUse(c, http.StatusConflict, "", "このアプリには、mam が管理するそのサーバーがありません。「別のサーバー」を選んでください")
	default:
		_ = c.Error(err)
		status, msg := failure.Save(err)
		h.answerUse(c, status, "", msg)
	}
}

// answerUse draws the section "使うサーバー" as saved, with what the save did (notice) or why it was refused (problem).
func (h Handlers) answerUse(c *gin.Context, status int, notice, problem string) {
	u := UseData(c.Request.Context(), h.svc)
	u.Notice, u.Error = notice, problem
	render(c, status, llmcard.UseSection(u))
}

// SaveConnection stores the form of "別のサーバー" (PUT /model-manager/local-llm/connection; fields base_url, api_key, clear_key, unload, and unload_present, which says that the form carried
// the switch) and answers with the section: 200 with the notice, or 422 with the reason and what was typed (the key is not drawn back). The server in use is not changed.
func (h Handlers) SaveConnection(c *gin.Context) {
	noStore(c)
	in := llmlocal.SaveInput{BaseURL: c.PostForm("base_url"), APIKey: c.PostForm("api_key"), ClearKey: c.PostForm("clear_key") == "1"}
	if c.PostForm("unload_present") == "1" {
		on := c.PostForm("unload") == "1"
		in.Unload = &on
	}
	err := h.svc.Save(in)
	d := ConnectionData(h.svc)
	status := http.StatusOK
	var invalid *llmlocal.InvalidError
	var store *credential.StoreError
	switch {
	case err == nil:
		d.Notice = saved
	case errors.As(err, &invalid):
		status, d.Error, d.Blame = http.StatusUnprocessableEntity, invalid.Message, invalid.Field
		if in.BaseURL != "" {
			d.BaseURL = in.BaseURL
		}
		d.Unload = in.Unload != nil && *in.Unload
	case errors.Is(err, llmlocal.ErrPinned):
		status, d.Error = http.StatusConflict, pinned
	case errors.As(err, &store):
		_ = c.Error(err)
		status, d.Error, d.Blame = http.StatusServiceUnavailable, store.Message, "api_key"
	case errors.Is(err, credential.ErrUnavailable):
		_ = c.Error(err)
		status, d.Error, d.Blame = http.StatusServiceUnavailable, keyStore, "api_key"
	default:
		_ = c.Error(err)
		status, d.Error = failure.Save(err)
	}
	render(c, status, llmcard.ConnectionSection(d))
}

// TestConnection tries the server of the form (POST /model-manager/local-llm/connection/test; fields base_url and api_key, empty for the saved ones) and answers with the result alone:
// 200 with the models, or 200 with the common error of a failure (a server that does not answer is a result, not a failure of this request); 422 for an address the rules refuse.
// While the calls go to a server of mam, the form is not tried: the answer says where that server stands.
func (h Handlers) TestConnection(c *gin.Context) {
	noStore(c)
	res, err := h.svc.Test(c.Request.Context(), llmlocal.TestInput{BaseURL: c.PostForm("base_url"), APIKey: c.PostForm("api_key")})
	var invalid *llmlocal.InvalidError
	switch {
	case errors.As(err, &invalid):
		render(c, http.StatusUnprocessableEntity, llmcard.Result(llmcard.TestResult{Message: invalid.Message}))
	case err != nil:
		_ = c.Error(err)
		render(c, http.StatusInternalServerError, llmcard.Result(llmcard.TestResult{Message: "接続テストを実行できませんでした"}))
	default:
		render(c, http.StatusOK, llmcard.Result(resultOf(res)))
	}
}

// noStore keeps an answer that is only true for now out of every cache.
func noStore(c *gin.Context) { c.Header("Cache-Control", "no-store") }

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
