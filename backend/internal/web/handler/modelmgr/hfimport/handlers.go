// Package hfimport holds the handlers of the import from Hugging Face on the model manager screen (spec/api/endpoints-model-manager.md,
// "Hugging Face"): the search, a repository with its files, the install of a file, the files the user added, and the Hugging
// Face token. It is a package of its own because the handler/modelmgr package is at its size limit. Every action calls
// modelmanager.Service, which enforces the rules (the formats, the terms, the age gate, the pins); the screen only asks.
package hfimport

import (
	"context"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// searchLimit is how many repositories a search shows.
const searchLimit = 20

// Handlers answer the routes of the import.
type Handlers struct {
	svc *modelmanager.Service
	// trial draws the trial section again: an import that ended (or a model that was deleted) changed the models a trial can use.
	trial func(ctx context.Context) view.TrialPanel
}

// Register adds the routes of the import to the engine.
func Register(engine *gin.Engine, svc *modelmanager.Service, trial func(ctx context.Context) view.TrialPanel) {
	h := Handlers{svc: svc, trial: trial}
	engine.GET(paths.ModelManagerPart("hf"), h.Search)
	engine.POST(paths.ModelManagerPart("hf", "install"), h.Install)
	engine.GET(paths.ModelManagerPart("hf", "models"), h.Models)
	engine.DELETE(paths.ModelManagerPart("hf", "models"), h.DeleteModel)
	engine.POST(paths.ModelManagerPart("hf", "jobs", ":job", "cancel"), h.CancelJob)
	engine.PUT(paths.ModelManagerPart("hf-token"), h.SetToken)
	engine.DELETE(paths.ModelManagerPart("hf-token"), h.ClearToken)
}

// Panel is the section on the first draw of the screen (an import that is running keeps the list polling).
func Panel(ctx context.Context, svc *modelmanager.Service) view.HFImport {
	return view.HFImport{Models: ListData(ctx, svc, nil)}
}

// refuse is the status and message of an error of the service: 422 for an input that cannot be taken, 409 when the state
// does not allow it, 404 for something that is not there; anything else is a fault (500, logged, with a generic message).
func refuse(c *gin.Context, err error) (int, string) {
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

// isRepoInput says whether what the user typed names one repository (a URL or owner/repo) rather than a keyword.
func isRepoInput(q string) bool { return strings.Contains(q, "://") || hf.ValidRepo(q) }

// Search answers GET /model-manager/hf with the files of the repository a URL or owner/repo names, or the repositories a
// keyword finds (field q, and kind for the keyword search).
func (h Handlers) Search(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query(view.FieldQuery))
	var r view.HFResult
	status := http.StatusOK
	var err error
	switch {
	case q == "":
		status, r.Error = http.StatusUnprocessableEntity, "キーワード・owner/repo・URL のどれかを入力してください"
	case isRepoInput(q):
		var v modelmanager.HFRepoView
		if v, err = h.svc.HFRepo(ctx, q); err == nil {
			repo := RepoOf(v)
			r.Repo = &repo
		}
	default:
		var hits []hf.SearchHit
		if hits, err = h.svc.SearchHF(ctx, q, c.Query(view.FieldKind), searchLimit); err == nil {
			r.Hits = HitsOf(hits)
			if len(r.Hits) == 0 {
				r.Notice = "見つかりませんでした。別のキーワードか、Hugging Face の URL で試してください"
			}
		}
	}
	if err != nil {
		status, r.Error = refuse(c, err)
	}
	render(c, status, view.HFResultView(r))
}

// Install answers POST /model-manager/hf/install: it queues the install of one file (fields repo, commit, path, folder, name,
// agree and terms_version) and answers with the list of the user's files, which shows its progress. A refusal comes back
// as a message above the result (HX-Retarget), where the user is looking.
func (h Handlers) Install(c *gin.Context) {
	ctx := c.Request.Context()
	ref, err := h.svc.InstallHFFile(ctx, modelmanager.HFInstallInput{
		Repo: c.PostForm(view.FieldRepo), Commit: c.PostForm(view.FieldCommit), Path: c.PostForm(view.FieldPath),
		Folder: c.PostForm(view.FieldFolder), Name: strings.TrimSpace(c.PostForm(view.FieldName)),
		Accept: c.PostForm(view.FieldAgree) == "1", TermsVersion: c.PostForm(view.FieldTerms),
	})
	if err != nil {
		status, msg := refuse(c, err)
		c.Header("HX-Retarget", "#"+view.HFMsgID)
		c.Header("HX-Reswap", "innerHTML")
		render(c, status, view.HFMsg(msg))
		return
	}
	l := ListData(ctx, h.svc, []string{ref.JobID})
	l.Notice = "導入を始めました。この画面を閉じても続きます"
	if ref.Existing {
		l.Notice = "このファイルは、すでに導入中です"
	}
	render(c, http.StatusAccepted, view.HFModelsWithMsgClear(l))
}

// Models answers GET /model-manager/hf/models, which the list polls while an import runs (query jobs: the ids it reports on).
func (h Handlers) Models(c *gin.Context) {
	ctx := c.Request.Context()
	var ids []string
	if q := c.Query("jobs"); q != "" {
		ids = strings.Split(q, ",")
	}
	l := ListData(ctx, h.svc, ids)
	if !l.Polling() && len(ids) > 0 {
		trial := h.trial(ctx)
		trial.OOB = true
		render(c, http.StatusOK, view.HFModelsWithTrial(l, trial))
		return
	}
	render(c, http.StatusOK, view.HFModelListView(l))
}

// CancelJob answers POST /model-manager/hf/jobs/:job/cancel.
func (h Handlers) CancelJob(c *gin.Context) {
	id := c.Param("job")
	err := h.svc.CancelInstall(id)
	l := ListData(c.Request.Context(), h.svc, []string{id})
	status := http.StatusOK
	if err != nil {
		status, l.Error = refuse(c, err)
	} else {
		l.Notice = "導入を中断しています"
	}
	render(c, status, view.HFModelListView(l))
}

// DeleteModel answers DELETE /model-manager/hf/models?path=: the file the user added and the workflows made for it go; a file a
// catalog model also holds stays while that model is installed.
func (h Handlers) DeleteModel(c *gin.Context) {
	ctx := c.Request.Context()
	err := h.svc.DeleteUserModel(ctx, c.Query(view.FieldPath))
	l := ListData(ctx, h.svc, nil)
	if err != nil {
		status, msg := refuse(c, err)
		l.Error = msg
		render(c, status, view.HFModelListView(l))
		return
	}
	l.Notice = "削除しました。カタログのモデルと共有しているファイルは、そのモデルが残る間は残ります"
	trial := h.trial(ctx)
	trial.OOB = true
	render(c, http.StatusOK, view.HFModelsWithTrial(l, trial))
}

// TokenData is the state of the Hugging Face token: whether it is set and its last characters, never the token.
func TokenData(svc *modelmanager.Service) view.HFToken {
	configured, last, err := svc.HuggingFaceTokenStatus()
	if err != nil {
		return view.HFToken{Unavailable: true}
	}
	return view.HFToken{Configured: configured, LastChars: last}
}

// SetToken stores the Hugging Face token (PUT /model-manager/hf-token, field token) and answers with its state. The
// token is not in the answer, in a log or in a header.
func (h Handlers) SetToken(c *gin.Context) {
	err := h.svc.SetHuggingFaceToken(c.PostForm(view.FieldToken))
	d := TokenData(h.svc)
	status := http.StatusOK
	if err != nil {
		status, d.Error = refuse(c, err)
	} else {
		d.Notice = "トークンを保存しました"
	}
	render(c, status, view.HFCard(d))
}

// ClearToken removes the Hugging Face token (DELETE /model-manager/hf-token).
func (h Handlers) ClearToken(c *gin.Context) {
	err := h.svc.ClearHuggingFaceToken()
	d := TokenData(h.svc)
	status := http.StatusOK
	if err != nil {
		status, d.Error = refuse(c, err)
	} else {
		d.Notice = "トークンを削除しました"
	}
	render(c, status, view.HFCard(d))
}
