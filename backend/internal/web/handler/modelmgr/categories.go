package modelmgr

import (
	"context"
	"net/http"

	"github.com/a-h/templ"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/hfimport"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/localllm"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/overview"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/runtimes"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/llmcard"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// has says whether this build has the category: the categories of the local runtimes, the local LLM, the custom nodes and the face-check models need their service. A category
// that is not here has no route (404), no card on the overview and no entry in the menu.
func (h Handlers) has(slug string) bool {
	switch slug {
	case mmcat.LocalRuntimes:
		return h.Runtimes != nil
	case mmcat.LocalLLM:
		return h.LLMLocal != nil
	case mmcat.CustomNodes:
		return h.Nodes != nil
	case mmcat.FaceCheck:
		return h.Gentest != nil && h.Gentest.FaceModels != nil
	}
	return true
}

// available are the categories of this build, in the order of mmcat.All.
func (h Handlers) available() []mmcat.Category {
	var out []mmcat.Category
	for _, c := range mmcat.All() {
		if h.has(c.Slug) {
			out = append(out, c)
		}
	}
	return out
}

// registerCategories adds GET /model-manager/<slug> for each category of the build, one route each (not a ":slug" parameter: a part of the screen such as
// "engine" or "models" must never be taken for a category).
func (h Handlers) registerCategories(engine *gin.Engine) {
	for _, c := range h.available() {
		engine.GET(c.Href(), h.Category(c))
	}
}

// isBodyRequest says the request is HTMX's and not the restore of a history entry (which wants the whole page).
func isBodyRequest(c *gin.Context) bool {
	return c.GetHeader("HX-Request") == "true" && c.GetHeader("HX-History-Restore-Request") != "true"
}

// Page answers GET /model-manager, the overview: the page, or the body alone for an HTMX request (a history restore wants the page).
func (h Handlers) Page(c *gin.Context) {
	o := h.overview(c)
	c.Writer.Header().Add("Vary", "HX-Request, HX-History-Restore-Request")
	if isBodyRequest(c) {
		render(c, http.StatusOK, view.OverviewBody(o))
		return
	}
	render(c, http.StatusOK, pages.ModelManagerPage(middleware.CSRFToken(c), o))
}

// overview reads the overview: the categories of this build and how each stands.
func (h Handlers) overview(c *gin.Context) view.Overview {
	d := overview.Deps{Svc: h.Svc, Runtimes: h.Runtimes, Nodes: h.Nodes, LLM: h.LLMLocal}
	if h.Gentest != nil {
		d.Face = h.Gentest.FaceModels
	}
	return overview.Build(c.Request.Context(), d, h.available())
}

// OverviewPart answers GET /model-manager/overview, which the overview polls while an install runs and its refresh button reads: the overview alone.
func (h Handlers) OverviewPart(c *gin.Context) {
	render(c, http.StatusOK, view.OverviewView(h.overview(c)))
}

// Category answers GET /model-manager/<slug> for a category: the page, or the body alone for an HTMX request (a history restore wants the page).
func (h Handlers) Category(cat mmcat.Category) gin.HandlerFunc {
	return func(c *gin.Context) {
		d := h.categoryData(c, cat.Slug)
		c.Writer.Header().Add("Vary", "HX-Request, HX-History-Restore-Request")
		if isBodyRequest(c) {
			render(c, http.StatusOK, view.Body(cat.Slug, d))
			return
		}
		render(c, http.StatusOK, pages.ModelManagerCategoryPage(middleware.CSRFToken(c), cat, h.available(), d))
	}
}

// categoryData is what the sections of the category draw (and nothing of the other categories is read).
func (h Handlers) categoryData(c *gin.Context, slug string) view.Data {
	ctx := c.Request.Context()
	var d view.Data
	switch slug {
	case mmcat.ComfyUI:
		d.Engine = engineData(ctx, h.Svc, "", false)
	case mmcat.ImageVideo:
		d.Adult, d.Commercial, d.Catalog = adultData(h.Svc), commercialData(h.Svc), catalogData(c, h.Svc, nil)
		d.EngineNeeded = engineNeeded(h)
	case mmcat.CustomNodes:
		d.Nodes = nodes.Section(ctx, h.Nodes)
	case mmcat.LocalRuntimes:
		d.Runtimes = runtimes.Section(ctx, h.Runtimes, false)
	case mmcat.LocalLLM:
		d.LLMUse, d.LLMRuntimes, d.LLMConnection = llmSections(ctx, h)
	case mmcat.FaceCheck:
		d.FaceModels = h.faceModels()
	case mmcat.HFExternal:
		d.HF, d.HFImport, d.External = hfimport.TokenData(h.Svc), hfimport.Panel(ctx, h.Svc), view.External{Endpoints: h.externalEndpoints(c)}
	}
	return d
}

// engineNeeded says the bundled engine is not installed or not running: the models page says so above its list.
func engineNeeded(h Handlers) bool {
	ev := h.Svc.Engine()
	return !ev.Installed || ev.Broken || string(ev.Process.State) != "running"
}

// llmSections are the sections of the page "ローカル LLM": the server in use, the servers of mam (their cards; left out when the app has no runtimes) and "別のサーバー".
func llmSections(ctx context.Context, h Handlers) (use, servers, connection templ.Component) {
	return llmcard.UseSection(localllm.UseData(ctx, h.LLMLocal)), runtimes.Section(ctx, h.Runtimes, true), llmcard.ConnectionSection(localllm.ConnectionData(h.LLMLocal))
}
