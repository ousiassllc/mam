// Package llm holds the handlers of the "テキスト生成（LLM）" category of the settings screen
// (spec/components/settings-screen-categories.md §2): the data of the table, the modal that edits one Agent, the save and
// the reset of one Agent, and the save and the reset of the whole assignment (PUT / DELETE /settings/llm, which the MCP
// tools and the tests keep using). It is a package of its own because handler is at its size limit; like handler it
// depends on the service layer and the web layer only.
package llm

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/llm"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

const (
	restartNeeded = "反映にはアプリの再起動が必要です"
	pinnedReason  = "起動オプションで固定されているため、この画面では変更できません"
	writeFailed   = "設定ファイルを書き込めませんでした。保存先の権限と空き容量を確認してください"
	notFound      = "対象が見つかりません"
	modalTitle    = "LLM の割り当て"
)

// SectionData is the saved assignment as the category shows it (the page handler of the settings screen calls it). It is
// what the next start runs with, not what runs now.
func SectionData(svc *llmassign.Service) view.Data {
	v := svc.View()
	d := view.Data{Providers: v.Providers, Pinned: v.Pinned, Problem: v.Problem, MaxModelLength: llmassign.MaxModelBytes}
	for _, e := range v.Entries {
		if e.Unused {
			d.Unused = append(d.Unused, rowOf(e))
		} else {
			d.Rows = append(d.Rows, rowOf(e))
		}
	}
	return d
}

func rowOf(e llmassign.Entry) view.Row {
	return view.Row{
		Agent: e.Agent, Label: e.Label, Purpose: purposes[e.Agent],
		Primary: target(e.Route.Primary), Fallback: target(e.Route.Fallback),
		DefaultPrimary: target(e.Default.Primary), DefaultFallback: target(e.Default.Fallback),
	}
}

func target(t llmassign.Target) view.Target { return view.Target(t) }

// find is the entry of the Agent in the service's view.
func find(v llmassign.View, agent string) (llmassign.Entry, bool) {
	i := slices.IndexFunc(v.Entries, func(e llmassign.Entry) bool { return e.Agent == agent })
	if i < 0 {
		return llmassign.Entry{}, false
	}
	return v.Entries[i], true
}

func form(v llmassign.View, e llmassign.Entry) view.Form {
	return view.Form{Row: rowOf(e), Providers: v.Providers, MaxModelLength: llmassign.MaxModelBytes}
}

// Edit answers the modal that edits the Agent :agent (GET /settings/llm/agents/:agent/edit). An unknown Agent is 404 and a
// pinned file 409, each with a modal that says so. A request that is not HTMX (the address typed in, a reload) gets page,
// the category page.
func Edit(svc *llmassign.Service, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		v := svc.View()
		e, ok := find(v, c.Param("agent"))
		switch {
		case !ok:
			settingshttp.Render(c, http.StatusNotFound, modal.Notice(modalTitle, notFound))
		case v.Pinned:
			settingshttp.Render(c, http.StatusConflict, modal.Notice(e.Label+" の LLM", pinnedReason))
		default:
			settingshttp.Render(c, http.StatusOK, view.Modal(form(v, e)))
		}
	}
}

// SaveAgent stores the LLM of the Agent :agent (PUT /settings/llm/agents/:agent; fields "primary.provider", "primary.model",
// "fallback.provider", "fallback.model") for the modal that sent it. The Agent is saved with the saved routes of the others,
// checked together. The answer is the row replaced out of band, a toast and the event that closes the modal; 422 is the
// modal again with why and what was typed (the file is then untouched), 409 a pinned file, 404 an unknown Agent.
func SaveAgent(svc *llmassign.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		typed := llmassign.Route{Primary: formTarget(c, "primary"), Fallback: formTarget(c, "fallback")}
		change(c, svc, func(llmassign.Entry) llmassign.Route { return typed }, &typed, " の LLM を保存しました")
	}
}

// ResetAgent puts the Agent :agent back to the default (DELETE /settings/llm/agents/:agent): the saved route is the default
// again, which the file does not keep. It answers like SaveAgent.
func ResetAgent(svc *llmassign.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		change(c, svc, func(e llmassign.Entry) llmassign.Route { return e.Default }, nil, " の LLM を既定に戻しました")
	}
}

// change saves the route that route makes of the Agent of the request and answers. typed, when not nil, is what the modal's
// form held: it is drawn again when the save is refused.
func change(c *gin.Context, svc *llmassign.Service, route func(llmassign.Entry) llmassign.Route, typed *llmassign.Route, done string) {
	agent := c.Param("agent")
	v := svc.View()
	e, ok := find(v, agent)
	switch {
	case !ok:
		settingshttp.Render(c, http.StatusNotFound, modal.Notice(modalTitle, notFound))
		return
	case v.Pinned:
		settingshttp.Render(c, http.StatusConflict, modal.Notice(e.Label+" の LLM", pinnedReason))
		return
	}
	err := svc.Save(map[string]llmassign.Route{agent: route(e)})
	if err == nil {
		now := svc.View()
		saved, _ := find(now, agent)
		row := rowOf(saved)
		row.Restart = true
		settingshttp.CloseModal(c)
		settingshttp.Render(c, http.StatusOK, templ.Join(view.RowOOB(row), atoms.SuccessToastOOB(e.Label+done+"。"+restartNeeded)))
		return
	}
	f := form(v, e)
	if typed != nil {
		f.Row.Primary, f.Row.Fallback = target(typed.Primary), target(typed.Fallback)
	}
	var invalid *llmassign.InvalidError
	status := http.StatusUnprocessableEntity
	switch {
	case errors.As(err, &invalid):
		f.Error = invalid.Message
		if invalid.Role != "" {
			f.Blame = invalid.Role + "." + invalid.Field
		}
	case errors.Is(err, llmassign.ErrPinned): // pinned between the view and the save
		settingshttp.Render(c, http.StatusConflict, modal.Notice(e.Label+" の LLM", pinnedReason))
		return
	default:
		_ = c.Error(err)
		status, f.Error = http.StatusInternalServerError, writeFailed
	}
	settingshttp.Render(c, status, view.Modal(f))
}

// formTarget is the provider and the model of the target role ("primary" / "fallback") in the form.
func formTarget(c *gin.Context, role string) llmassign.Target {
	return llmassign.Target{Provider: c.PostForm(role + ".provider"), Model: strings.TrimSpace(c.PostForm(role + ".model"))}
}

// Save stores the whole assignment of the form (PUT /settings/llm; fields "<agent>.primary.provider",
// "<agent>.primary.model", "<agent>.fallback.provider", "<agent>.fallback.model") and answers with the category's table:
// 200 with the notice, or 422 with the reason when it cannot be saved (the file is then untouched). The screen itself saves
// one Agent at a time (SaveAgent); this is for the MCP tools' and the tests' whole saves.
func Save(svc *llmassign.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		routes := map[string]llmassign.Route{}
		for _, a := range llmassign.Agents() {
			routes[a] = llmassign.Route{Primary: formTarget(c, a+".primary"), Fallback: formTarget(c, a+".fallback")}
		}
		answer(c, svc, svc.Save(routes), "保存しました。"+restartNeeded)
	}
}

// Reset drops the saved assignment (DELETE /settings/llm), so that the defaults apply from the next start, and answers with
// the table.
func Reset(svc *llmassign.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		answer(c, svc, svc.Reset(), "既定に戻しました。"+restartNeeded)
	}
}

// answer renders the table after an operation on the whole assignment.
func answer(c *gin.Context, svc *llmassign.Service, err error, notice string) {
	d := SectionData(svc)
	status := http.StatusOK
	var invalid *llmassign.InvalidError
	switch {
	case err == nil:
		d.Notice = notice
	case errors.As(err, &invalid):
		status, d.Error = http.StatusUnprocessableEntity, invalid.Message
	case errors.Is(err, llmassign.ErrPinned):
		status, d.Error = http.StatusConflict, pinnedReason
	default:
		_ = c.Error(err)
		status, d.Error = http.StatusInternalServerError, writeFailed
	}
	settingshttp.Render(c, status, view.Section(d))
}

// Register adds the routes of the category: the modal (GET paths.LLMAgentEdit), the save and the reset of one Agent (PUT /
// DELETE paths.LLMAgent) and of the whole assignment (PUT / DELETE paths.LLMSettings). page answers the modal route for a
// request that is not HTMX.
func Register(engine *gin.Engine, svc *llmassign.Service, page gin.HandlerFunc) {
	engine.GET(paths.LLMAgentEdit(":agent"), Edit(svc, page))
	engine.PUT(paths.LLMAgent(":agent"), SaveAgent(svc))
	engine.DELETE(paths.LLMAgent(":agent"), ResetAgent(svc))
	engine.PUT(paths.LLMSettings(), Save(svc))
	engine.DELETE(paths.LLMSettings(), Reset(svc))
}
