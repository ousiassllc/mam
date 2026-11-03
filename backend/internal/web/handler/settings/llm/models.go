package llm

import (
	"context"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
)

// form is the modal of an Agent drawn from the row held in e (typed: the model names are what the modal's form held, which
// a refusal draws again; the saved row otherwise). The model pull-downs list the models of each target's provider.
func form(ctx context.Context, models *llmmodels.Service, v llmassign.View, e llmassign.Entry) view.Form {
	row := rowOf(e)
	return view.Form{
		Row: row, Providers: v.Providers, MaxModelLength: llmassign.MaxModelBytes,
		Primary:  modelField(ctx, models, e.Agent, "primary", row.Primary.Provider, row.Primary.Model, row.DefaultPrimary),
		Fallback: modelField(ctx, models, e.Agent, "fallback", row.Fallback.Provider, row.Fallback.Model, row.DefaultFallback),
	}
}

// modelField is the model pull-down of a target: the models of provider, with selected chosen (the first model, or the
// default's when the provider is the default's, when selected is empty). A selected model the provider does not list is
// kept as a choice and marked unlisted.
func modelField(ctx context.Context, models *llmmodels.Service, agent, role, provider, selected string, def view.Target) view.ModelField {
	f := view.ModelField{Agent: agent, Role: role, Provider: provider, Default: def, Selected: selected}
	list, err := models.List(ctx, provider)
	if err != nil { // a provider the form did not offer: nothing to choose from
		f.Reason, f.Unlisted = "このプロバイダーのモデルを一覧にできません", selected != ""
		return f
	}
	f.FromAPI, f.Reason = list.Source == llmmodels.SourceAPI, list.Fallback.Message()
	for _, m := range list.Models {
		f.Options = append(f.Options, view.ModelOption{ID: m.ID, Name: m.Name})
	}
	switch {
	case f.Selected == "" && def.Provider == provider && list.Has(def.Model):
		f.Selected = def.Model
	case f.Selected == "" && len(f.Options) > 0:
		f.Selected = f.Options[0].ID
	}
	f.Unlisted = f.Selected != "" && !list.Has(f.Selected)
	return f
}

// ModelField answers the model pull-down of one target of the Agent :agent for the provider the form just chose (GET
// /settings/llm/agents/:agent/models/:role, "<role>.provider"): the inside of the model box, which replaces the old one. The
// model saved for the Agent stays chosen while it is the same provider; for another provider the default's model, else
// the first. A request that is not HTMX gets page.
func ModelField(svc *llmassign.Service, models *llmmodels.Service, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		role := c.Param("role")
		e, ok := find(svc.View(), c.Param("agent"))
		if !ok || (role != "primary" && role != "fallback") {
			c.String(http.StatusNotFound, notFound)
			return
		}
		provider := c.Query(role + ".provider")
		if !slices.Contains(svc.View().Providers, provider) {
			c.String(http.StatusUnprocessableEntity, "未対応のプロバイダーです")
			return
		}
		saved, def := target(e.Route.Primary), target(e.Default.Primary)
		if role == "fallback" {
			saved, def = target(e.Route.Fallback), target(e.Default.Fallback)
		}
		selected := ""
		if saved.Provider == provider {
			selected = saved.Model
		}
		c.Header("Cache-Control", "no-store")
		f := view.Form{MaxModelLength: llmassign.MaxModelBytes}
		settingshttp.Render(c, http.StatusOK, view.ModelFieldBody(f, modelField(c.Request.Context(), models, e.Agent, role, provider, selected, def)))
	}
}
