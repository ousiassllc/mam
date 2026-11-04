// Package llmlocal holds the handlers of the card "ローカル LLM サーバー" of the category "テキスト生成（LLM）" of the settings screen (#1008): the data of
// the card, its save (PUT /settings/llm/local) and the connection test (POST /settings/llm/local/test). It is a package of its own because handler
// is at its size limit; like handler it depends on the service layer and the web layer only. The API key is read from the form, handed to the
// service and never drawn again.
package llmlocal

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/web/handler/failure"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llmlocal"
)

const (
	saved    = "保存しました。次の LLM の呼び出しからすぐ使われます（再起動は要りません）"
	pinned   = "起動オプションで設定ファイルが固定されているため、ベース URL とスイッチは変えられません"
	keyStore = "API キーを OS のセキュアストレージに保存できませんでした。キーストアを確認してください"
)

// CardData is the saved settings as the card shows them.
func CardData(svc *llmlocal.Service) view.Data {
	v := svc.View()
	d := view.Data{
		BaseURL: v.BaseURL, DefaultBaseURL: v.DefaultBaseURL, KeyConfigured: v.KeyConfigured, KeyHint: v.KeyHint, KeyUnreadable: v.KeyUnreadable,
		Unload: v.UnloadBeforeGeneration, UseManaged: v.UseManaged, Pinned: v.Pinned, Problem: v.Problem,
	}
	if info, ok := svc.ManagedInfo(context.Background()); ok { // where it stands: nothing is started
		d.Managed = &view.Managed{State: string(info.State), Reason: info.Reason, Model: info.ModelName}
	}
	return d
}

// Save stores the form of the card (PUT /settings/llm/local; fields base_url, api_key, clear_key, unload, use_managed, and unload_present / use_managed_present, which say that the form
// carried the switch) and answers with the card: 200 with the notice, or 422 with the reason and what was typed (the key is not drawn back).
func Save(svc *llmlocal.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		settingshttp.NoStore(c)
		in := llmlocal.SaveInput{BaseURL: c.PostForm("base_url"), APIKey: c.PostForm("api_key"), ClearKey: c.PostForm("clear_key") == "1"}
		if c.PostForm("unload_present") == "1" {
			on := c.PostForm("unload") == "1"
			in.Unload = &on
		}
		if c.PostForm("use_managed_present") == "1" {
			on := c.PostForm("use_managed") == "1"
			in.UseManaged = &on
		}
		err := svc.Save(in)
		d := CardData(svc)
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
		case errors.Is(err, llmlocal.ErrManagedUnavailable):
			status, d.Error = http.StatusUnprocessableEntity, "このアプリには、mam が管理する llama.cpp がありません"
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
		settingshttp.Render(c, status, view.Card(d))
	}
}

// Test tries the server of the form (POST /settings/llm/local/test; fields base_url and api_key, empty for the saved ones) and answers with the result alone:
// 200 with the models, or 200 with the common error of a failure (a server that does not answer is a result, not a failure of this request); 422 for an
// address the rules refuse.
func Test(svc *llmlocal.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		settingshttp.NoStore(c)
		res, err := svc.Test(c.Request.Context(), llmlocal.TestInput{BaseURL: c.PostForm("base_url"), APIKey: c.PostForm("api_key")})
		var invalid *llmlocal.InvalidError
		switch {
		case errors.As(err, &invalid):
			settingshttp.Render(c, http.StatusUnprocessableEntity, view.Result(view.TestResult{Message: invalid.Message}))
		case err != nil:
			_ = c.Error(err)
			settingshttp.Render(c, http.StatusInternalServerError, view.Result(view.TestResult{Message: "接続テストを実行できませんでした"}))
		default:
			settingshttp.Render(c, http.StatusOK, view.Result(resultOf(res)))
		}
	}
}

func resultOf(r llmlocal.TestResult) view.TestResult {
	out := view.TestResult{OK: r.OK, Message: r.Message, Models: r.Models, Count: r.Count}
	if r.Error != nil {
		v := r.Error.View(apperr.LangJa)
		out.Failure = &v
	}
	return out
}
