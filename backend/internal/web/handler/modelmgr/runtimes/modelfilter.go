package runtimes

import (
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices"
	"github.com/ousiassllc/mam/backend/internal/web/handler/pickfilter"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	card "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/runtimecard"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// modelScreen is the filter of the models of one runtime (#1011): the same bar, panel and chips as the model manager's list, remembered per runtime. The screen
// starts at the zero condition (every model of a runtime can be chosen; "選べるものだけ" has nothing to hide). The panel offers what tells this runtime's models apart:
// the memory and the license always, the kind and the language only where its models differ in them (every model of Irodori-TTS is a Japanese voice, so asking
// for 「音声」「日本語」 there would only repeat what the card says; across runtimes the MCP tool list_local_runtime_models filters by both), and the
// uncensored flag where one of them has it. The models that are ticked travel with the request of the bar: the list is drawn again with the user's choice and
// acceptances kept.
func modelScreen(id string, views []runtimechoices.View) pickfilter.Screen {
	form := "#" + card.ModelsID(id)
	return pickfilter.Screen{
		Key: "runtime-models-" + id, BarID: card.ModelBarID(id), Action: paths.ModelManagerPart("runtimes", id, "models"), Target: "#" + card.ModelListID(id),
		Include: form + " input[name=" + card.FieldModel + "], " + form + " input[name^=" + card.FieldAcceptPrefix + "]",
		Start:   modelfilter.Condition{},
		Facets:  modelFacets(views),
	}
}

// modelFacets are the sections of the panel for the models of a runtime.
func modelFacets(views []runtimechoices.View) modelpick.Facets {
	f := modelpick.Facets{Commercial: true, VRAM: true}
	var kinds []modelfilter.Kind
	var langs []string
	for _, v := range views {
		it := v.Item()
		for _, k := range it.Kinds {
			if !slices.Contains(kinds, k) {
				kinds = append(kinds, k)
			}
		}
		for _, l := range it.Languages {
			if !slices.Contains(langs, l) {
				langs = append(langs, l)
			}
		}
		f.Uncensored = f.Uncensored || v.Uncensored
	}
	if len(kinds) > 1 {
		f.Kinds = kinds
	}
	slices.Sort(langs)
	f.Languages = slices.DeleteFunc(langs, func(l string) bool {
		return !slices.ContainsFunc(views, func(v runtimechoices.View) bool { return !slices.Contains(v.Languages, l) }) // every model speaks it: nothing to tell apart
	})
	return f
}

// filterModels narrows the models of the runtime's card by the condition of the request (the bar's query, else what the runtime remembered) and draws the
// filter from it. A request of the bar (it carries the marker) is remembered.
func (h Handlers) filterModels(c *gin.Context, r *card.Runtime) {
	views, err := runtimechoices.Service{Runtimes: h.svc}.Views(c.Request.Context(), r.ID)
	if err != nil || len(views) == 0 {
		return
	}
	screen := modelScreen(r.ID, views)
	cond, problem := screen.Read(c)
	screen.Remember(c, cond)
	r.Models = cardModels(runtimechoices.Where(views, pickfilter.Resolve(cond)))
	r.ModelFilter = pickfilter.Sync(screen.Filter(cond, problem, pickfilter.CountOf(cond, views, runtimechoices.View.Item)), c)
}

// RuntimeModels answers GET /model-manager/runtimes/:id/models, the request of the filter bar of the models of a runtime: the list that passes the condition
// (the radio the user left on and the acceptances they ticked kept) and, out of band, the number on the funnel. A runtime that is not there is a 404.
func (h Handlers) RuntimeModels(c *gin.Context) {
	r, err := runtimeData(c.Request.Context(), h.svc, c.Param("id"), "", false)
	if err != nil {
		status, msg := refuseRuntime(c, err)
		c.String(status, msg)
		return
	}
	h.filterModels(c, &r)
	keepChoice(c, &r)
	render(c, http.StatusOK, card.RuntimeModelsFiltered(r))
}

// keepChoice puts back what the user had done in the form when the filter drew the list again: the model whose radio was on (if it is still listed; else the one in
// force is on, as the form opened) and the acceptances that were ticked. An acceptance is only kept when the version of the text that came back is the one that is
// shown (the user did not tick another text), and nothing ticks one that was not ticked.
func keepChoice(c *gin.Context, r *card.Runtime) {
	chosen := c.Query(card.FieldModel)
	if !slices.ContainsFunc(r.Models, func(m card.Model) bool { return m.ID == chosen }) {
		chosen = ""
	}
	for i := range r.Models {
		m := &r.Models[i]
		if chosen != "" {
			m.Selected = m.ID == chosen
		}
		if m.Consent != nil {
			m.Consent.Ticked = !m.Consent.Accepted && strings.TrimSpace(c.Query(card.FieldAcceptPrefix+m.ID)) == m.Consent.Version
		}
	}
}
