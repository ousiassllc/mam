package modelmgr

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

// hfData is the state of the Hugging Face token: whether it is set and its last characters, never the token.
func hfData(h Handlers) view.HFToken {
	configured, last, err := h.Svc.HuggingFaceTokenStatus()
	if err != nil {
		return view.HFToken{Unavailable: true}
	}
	return view.HFToken{Configured: configured, LastChars: last}
}

// SetToken stores the Hugging Face token (PUT /model-manager/hf-token, field token) and answers with its state. The
// token is not in the answer, in a log or in a header.
func (h Handlers) SetToken(c *gin.Context) {
	err := h.Svc.SetHuggingFaceToken(c.PostForm(view.FieldToken))
	d := hfData(h)
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
	err := h.Svc.ClearHuggingFaceToken()
	d := hfData(h)
	status := http.StatusOK
	if err != nil {
		status, d.Error = refuse(c, err)
	} else {
		d.Notice = "トークンを削除しました"
	}
	render(c, status, view.HFCard(d))
}

// externalEndpoints are the registered ComfyUI endpoints other than the bundled one.
func (h Handlers) externalEndpoints(c *gin.Context) []string {
	st, err := h.Svc.Status(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return nil
	}
	var names []string
	for _, ep := range st.Endpoints {
		if !ep.Bundled {
			names = append(names, ep.Name)
		}
	}
	return names
}

// External answers GET /model-manager/external (query endpoint): the endpoints to choose from and, for the chosen
// one, the model files its ComfyUI has and what a catalog model lacks there. It only reads: nothing is installed there.
func (h Handlers) External(c *gin.Context) {
	x := view.External{Endpoints: h.externalEndpoints(c), Selected: strings.TrimSpace(c.Query(view.FieldEndpoint))}
	if x.Selected == "" {
		render(c, http.StatusOK, view.ExternalCard(x))
		return
	}
	inv, err := h.Svc.ExternalInventory(c.Request.Context(), x.Selected)
	status := http.StatusOK
	if err != nil {
		var kind mmview.RefusalKind
		if kind, x.Error = mmview.Refuse(err); kind == mmview.RefusalNone {
			_ = c.Error(err)
			status, x.Error = http.StatusBadGateway, "ComfyUI に接続できませんでした。URL・起動の状態・API キーを確かめてください"
		} else {
			status, _ = refuse(c, err)
		}
		render(c, status, view.ExternalCard(x))
		return
	}
	res := &view.ExternalResult{Endpoint: inv.Endpoint, Version: inv.Version}
	for _, f := range []string{"checkpoints", "diffusion_models", "text_encoders", "vae"} {
		res.Folders = append(res.Folders, view.ExternalFolder{Name: f, Files: inv.Folders[f]})
	}
	for _, m := range inv.Models {
		res.Models = append(res.Models, view.ExternalModel{Name: m.Name, Complete: m.Complete, Present: m.Present, Missing: m.Missing})
	}
	x.Result = res
	render(c, status, view.ExternalCard(x))
}
