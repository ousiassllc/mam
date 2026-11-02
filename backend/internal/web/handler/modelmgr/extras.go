package modelmgr

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/modeldetail"
)

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

// ExternalDetail answers GET /model-manager/external/detail (query endpoint, name) with the modal of the card of a model on an external
// ComfyUI: the files it has there and the ones it lacks. It asks the ComfyUI again (nothing is kept of the last check); one that cannot be
// reached, or a model that is not in its answer, is the reason in the modal.
func (h Handlers) ExternalDetail(c *gin.Context) {
	endpoint, name := strings.TrimSpace(c.Query(view.FieldEndpoint)), c.Query(view.FieldName)
	inv, err := h.Svc.ExternalInventory(c.Request.Context(), endpoint)
	if err != nil {
		status, msg := http.StatusBadGateway, "ComfyUI に接続できませんでした。URL・起動の状態・API キーを確かめてください"
		if kind, _ := mmview.Refuse(err); kind != mmview.RefusalNone {
			status, msg = refuse(c, err)
		} else {
			_ = c.Error(err)
		}
		render(c, status, modal.Notice("外部の ComfyUI のモデル", msg))
		return
	}
	for _, m := range inv.Models {
		if m.Name == name {
			render(c, http.StatusOK, modeldetail.ExternalDetail(inv.Endpoint, view.ExternalModel{Name: m.Name, Complete: m.Complete, Present: m.Present, Missing: m.Missing}))
			return
		}
	}
	render(c, http.StatusNotFound, modal.Notice("外部の ComfyUI のモデル", "対象が見つかりません。画面を読み込み直してください"))
}
