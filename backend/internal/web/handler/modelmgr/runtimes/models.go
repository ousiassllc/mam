package runtimes

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/jobview"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	card "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/runtimecard"
)

// languageNames are how the screen names the languages of a model (the codes the catalog holds).
var languageNames = map[string]string{"ja": "日本語", "en": "英語", "zh": "中国語"}

// modelsOf is the models of a runtime for its card; empty for a runtime that has one only (or when the models cannot be read).
func modelsOf(ctx context.Context, svc runtimechoices.Service, id string) []card.Model {
	views, err := svc.Views(ctx, id)
	if err != nil {
		return nil
	}
	out := make([]card.Model, 0, len(views))
	for _, v := range views {
		var langs []string
		for _, l := range v.Languages {
			name, ok := languageNames[l]
			if !ok {
				name = l
			}
			langs = append(langs, name)
		}
		m := card.Model{
			ID: v.ID, Name: v.Name, Description: v.Description, Active: v.Active, Default: v.Default,
			Size: jobview.BytesText(v.SizeBytes), Languages: strings.Join(langs, "・"),
			VRAM:    jobview.ModelVRAMText(catalog.VRAM{MinGB: v.VRAMMinGB, RecommendedGB: v.VRAMRecommendedGB, Source: v.VRAMNote, Estimate: v.VRAMEstimate}),
			VRAMFit: v.VRAMFit, License: v.LicenseInfo.Name, LicenseURL: v.LicenseInfo.URL, Commercial: v.LicenseInfo.Commercial.Label(),
			Cautions: v.Cautions, Uncensored: v.Uncensored,
		}
		if v.Consent != nil {
			m.Consent = &card.ModelConsent{Version: v.Consent.Version, Text: v.Consent.Text, Accepted: v.Accepted}
		}
		out = append(out, m)
	}
	return out
}

// SetRuntimeModel chooses the model of a runtime (PUT /model-manager/runtimes/:id/model, field model; accept_<model> holds the version of
// the license text the user was shown, only when they ticked it) and answers with the list of models again, with what happened. The service
// refuses a model whose license is not accepted; this handler only passes on what the form carries.
func (h Handlers) SetRuntimeModel(c *gin.Context) {
	id, model := c.Param("id"), c.PostForm(card.FieldModel)
	svc := runtimechoices.Service{Runtimes: h.svc}
	var accept *runtimechoices.Accept
	if v := c.PostForm(card.FieldAcceptPrefix + model); v != "" {
		accept = &runtimechoices.Accept{Version: v}
	}
	status, msg := http.StatusOK, ""
	if err := svc.Select(c.Request.Context(), id, model, accept); err != nil {
		status, msg = refuseRuntime(c, err)
	}
	r, err := runtimeData(c.Request.Context(), h.svc, id, "", false)
	if err != nil {
		st, m := refuseRuntime(c, err)
		render(c, st, card.RuntimesSection(card.Runtimes{Message: view.Message{Error: m}, Items: items(c.Request.Context(), h.svc)}))
		return
	}
	if msg != "" {
		r.ModelsMessage.Error = msg
	} else {
		r.ModelsMessage.Notice = "使うモデルを変えました。次に起動するときから反映されます。重みが足りないときは、カードの「重みを導入」で取得してください"
	}
	render(c, status, card.RuntimeModels(r))
}
