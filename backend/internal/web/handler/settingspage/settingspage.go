// Package settingspage holds the pages of the global settings screen (spec/components/settings-screen.md §2–§5 and
// settings-screen-routes.md §1): the overview and the categories, each at /settings/<slug>. It is a package of its own
// because handler is at its size limit; like handler it depends on the service layer and the web layer only.
//
// A page is one handler for every category: it builds the body of the category and answers with the whole page, or with
// the body alone for an HTMX request (the polls and the re-reads after an action). The modal handlers of the screen that
// are asked for without HTMX (the address typed in, a reload) answer with the page of their category (Services.Page).
package settingspage

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/credential/probe"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/mcpsettings"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	updatehandler "github.com/ousiassllc/mam/backend/internal/web/handler/appupdate"
	chatgpthandler "github.com/ousiassllc/mam/backend/internal/web/handler/chatgptsettings"
	llmhandler "github.com/ousiassllc/mam/backend/internal/web/handler/llmassign"
	localhandler "github.com/ousiassllc/mam/backend/internal/web/handler/localmodels"
	mcphandler "github.com/ousiassllc/mam/backend/internal/web/handler/mcpsettings"
	modelhandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelassign"
	credentialshandler "github.com/ousiassllc/mam/backend/internal/web/handler/settings/credentials"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/settingsui"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Services are what the pages are built from. A nil service leaves its category out: no entry in the navigation, no
// card in the overview, and no route (the URL is 404). Keys, and so the API keys category and the overview, are always
// there; Update is nil in the headless server, whose version category then shows the version only.
type Services struct {
	Keys *service.CredentialService
	// Tester tries the API keys against their providers; nil leaves the connection test out.
	Tester  *probe.Tester
	ChatGPT *chatgpt.Service
	// LLM and Models are the assignments of the models: both or neither (the router was built with settings or without).
	LLM         *llmassign.Service
	Models      *modelsettings.Service
	LocalModels *localmodels.Service
	MCP         *mcpsettings.Service
	Update      *appupdate.Service
	// Fragments answer an HTMX request for the category of their slug instead of its body: the screens that are still
	// their own fragments (the local models panel, which its actions replace).
	Fragments map[string]gin.HandlerFunc
}

// category is one entry of the navigation: where it is, what it is called and what it needs.
type category struct {
	slug  string
	title string
	// shown is whether the category exists with these services.
	shown func(Services) bool
	body  func(*gin.Context, Services) templ.Component
}

func always(Services) bool { return true }

// categories are the pages of the screen in the order of the navigation (spec/components/settings-screen.md §2). The
// overview is first and has the slug SettingsOverview.
var categories []category

// init fills categories: the bodies refer back to the list (the overview reads the titles), so it cannot be a variable initializer.
func init() {
	categories = []category{
		{paths.SettingsOverview, "概要", always, overviewBody},
		{paths.SettingsAPIKeys, "API キーとログイン", always, apiKeysBody},
		{paths.SettingsLLM, "テキスト生成（LLM）", func(s Services) bool { return s.LLM != nil }, func(_ *gin.Context, s Services) templ.Component {
			return pages.SettingsLLMBody(llmhandler.SectionData(s.LLM))
		}},
		{paths.SettingsModels, "画像・動画モデル", func(s Services) bool { return s.Models != nil }, func(_ *gin.Context, s Services) templ.Component {
			return pages.SettingsModelsBody(modelhandler.SectionData(s.Models))
		}},
		{paths.SettingsLocalModels, "ローカル・代替モデル", func(s Services) bool { return s.LocalModels != nil }, func(_ *gin.Context, s Services) templ.Component {
			return pages.SettingsLocalModelsBody(localhandler.SectionData(s.LocalModels))
		}},
		{paths.SettingsMCP, "MCP サーバー", func(s Services) bool { return s.MCP != nil }, func(c *gin.Context, s Services) templ.Component {
			return pages.SettingsMCPBody(mcphandler.SectionData(c.Request.Context(), s.MCP))
		}},
		{paths.SettingsUpdate, "バージョンとアップデート", always, func(_ *gin.Context, s Services) templ.Component {
			return pages.SettingsUpdateBody(updatehandler.SectionData(s.Update))
		}},
	}
}

func apiKeysBody(c *gin.Context, s Services) templ.Component {
	keys := credentialshandler.Data(c, credentialshandler.Keys{Svc: s.Keys, Tester: s.Tester})
	if s.ChatGPT == nil {
		return pages.SettingsAPIKeysBody(keys, nil)
	}
	login := chatgpthandler.SectionData(s.ChatGPT)
	return pages.SettingsAPIKeysBody(keys, &login)
}

func find(slug string) *category {
	for i := range categories {
		if categories[i].slug == slug {
			return &categories[i]
		}
	}
	return nil
}

// Register adds GET /settings and GET /settings/<slug> of every category the services have.
func Register(engine *gin.Engine, s Services) {
	for _, cat := range categories {
		if cat.shown(s) {
			engine.GET(paths.SettingsCategory(cat.slug), s.Page(cat.slug))
		}
	}
}

// nav is the navigation with the category of slug marked as the current one: the categories this screen has.
func (s Services) nav(slug string) []settingsui.NavItem {
	var items []settingsui.NavItem
	for _, cat := range categories {
		if cat.shown(s) {
			items = append(items, settingsui.NavItem{Href: templ.SafeURL(paths.SettingsCategory(cat.slug)), Label: cat.title, Current: cat.slug == slug})
		}
	}
	return items
}

// Page is the handler of the page of the category slug: the whole page, or the body alone for an HTMX request. It is
// also what a modal route answers a request that is not HTMX with. A category the services do not have is 404.
func (s Services) Page(slug string) gin.HandlerFunc {
	return func(c *gin.Context) {
		cat := find(slug)
		if cat == nil || !cat.shown(s) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "no-store")
		htmx := settingshttp.IsHTMX(c)
		if fragment := s.Fragments[slug]; htmx && fragment != nil {
			fragment(c)
			return
		}
		body := cat.body(c, s)
		if htmx {
			settingshttp.Render(c, http.StatusOK, body)
			return
		}
		title := "設定"
		if slug != paths.SettingsOverview {
			title = "設定: " + cat.title
		}
		canonical := ""
		if own := paths.SettingsCategory(slug); c.Request.URL.Path != own {
			canonical = own
		}
		settingshttp.Render(c, http.StatusOK, pages.GlobalSettingsPage(middleware.CSRFToken(c), pages.GlobalSettingsPageData{
			Title: title, Canonical: canonical, Nav: s.nav(slug), Body: body,
		}))
	}
}
