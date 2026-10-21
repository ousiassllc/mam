// Package llmassign holds the handlers of the "LLM の割り当て" section of the global settings screen
// (spec/api/endpoints.md). It is a package of its own because handler is at its size limit; like handler,
// it depends on the service layer and the web layer only.
package llmassign

import (
	"errors"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/llmassign"
)

const (
	savedNotice = "保存しました。反映にはアプリの再起動が必要です"
	resetNotice = "既定に戻しました。反映にはアプリの再起動が必要です"
)

// SectionData is the saved assignment as the section shows it (the page handler of the settings screen
// calls it). It is what the next start runs with, not what runs now.
func SectionData(svc *llmassign.Service) view.Data {
	v := svc.View()
	d := view.Data{Providers: v.Providers, Pinned: v.Pinned, Problem: v.Problem, MaxModelLength: llmassign.MaxModelBytes}
	for _, e := range v.Entries {
		row := view.Row{
			Agent: e.Agent, Label: e.Label,
			Primary: target(e.Route.Primary), Fallback: target(e.Route.Fallback),
			DefaultPrimary: target(e.Default.Primary), DefaultFallback: target(e.Default.Fallback),
		}
		if e.Unused {
			d.Unused = append(d.Unused, row)
		} else {
			d.Rows = append(d.Rows, row)
		}
	}
	return d
}

func target(t llmassign.Target) view.Target { return view.Target(t) }

// Save stores the assignment of the form (PUT /settings/llm; fields "<agent>.primary.provider",
// "<agent>.primary.model", "<agent>.fallback.provider", "<agent>.fallback.model") and answers with the
// section: 200 with the notice, or 422 with the reason and what was typed when it cannot be saved (the
// file is then untouched).
func Save(svc *llmassign.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		routes := map[string]llmassign.Route{}
		for _, a := range llmassign.Agents() {
			routes[a] = llmassign.Route{
				Primary:  formTarget(c, a, "primary"),
				Fallback: formTarget(c, a, "fallback"),
			}
		}
		answer(c, svc, svc.Save(routes), savedNotice, routes)
	}
}

// Reset drops the saved assignment (DELETE /settings/llm), so that the defaults apply from the next start,
// and answers with the section.
func Reset(svc *llmassign.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		answer(c, svc, svc.Reset(), resetNotice, nil)
	}
}

func formTarget(c *gin.Context, a, role string) llmassign.Target {
	prefix := a + "." + role + "."
	return llmassign.Target{
		Provider: c.PostForm(prefix + "provider"),
		Model:    strings.TrimSpace(c.PostForm(prefix + "model")),
	}
}

// answer renders the section after an operation. typed is what the form held: shown again when the
// operation was refused, so that a mistake in one field does not discard the rest.
func answer(c *gin.Context, svc *llmassign.Service, err error, notice string, typed map[string]llmassign.Route) {
	d := SectionData(svc)
	status := http.StatusOK
	var invalid *llmassign.InvalidError
	switch {
	case err == nil:
		d.Notice = notice
	case errors.As(err, &invalid):
		status, d.Error = http.StatusUnprocessableEntity, invalid.Message
		withTyped(&d, typed)
	case errors.Is(err, llmassign.ErrPinned):
		status, d.Error = http.StatusConflict, "起動オプションで固定されているため、この画面では変更できません"
	default:
		_ = c.Error(err)
		status, d.Error = http.StatusInternalServerError, "設定ファイルを書き込めませんでした。保存先の権限と空き容量を確認してください"
		withTyped(&d, typed)
	}
	render(c, status, view.Panel(d))
}

// withTyped puts the routes of the form into the rows.
func withTyped(d *view.Data, typed map[string]llmassign.Route) {
	for _, rows := range [][]view.Row{d.Rows, d.Unused} {
		for i := range rows {
			if r, ok := typed[rows[i].Agent]; ok {
				rows[i].Primary, rows[i].Fallback = target(r.Primary), target(r.Fallback)
			}
		}
	}
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
