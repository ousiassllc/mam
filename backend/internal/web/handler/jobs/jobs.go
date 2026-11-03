// Package jobs holds the handlers of the job list screen (GET /jobs, spec/api/endpoints-jobs.md): the jobs that run now and the latest
// that ended, read from joblist.Service. It is a package of its own because handler is at its size limit; like handler it depends on the
// service layer and the web layer only. Every route reads: no job is changed from here.
package jobs

import (
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/joblist"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/jobs"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// readFailure is what the list says when the jobs could not be read; the cause goes to the log, not to the page.
const readFailure = "ジョブを読み込めませんでした。更新を押して、もう一度お試しください。直らないときは mam のログを確認してください。"

// Handlers answer the routes of the screen.
type Handlers struct {
	Svc *joblist.Service
}

// Register adds the routes of the screen to the engine.
func Register(engine *gin.Engine, svc *joblist.Service) {
	h := Handlers{Svc: svc}
	engine.GET(paths.Jobs(), h.Page)
	engine.GET(paths.JobsList(), h.List)
}

// Page answers GET /jobs.
func (h Handlers) Page(c *gin.Context) {
	render(c, pages.JobsPage(middleware.CSRFToken(c), h.list(c)))
}

// List answers GET /jobs/list: the list alone, which the refresh button and the polling swap in.
func (h Handlers) List(c *gin.Context) {
	render(c, view.Section(h.list(c)))
}

// list reads the jobs. A failure is answered as a 200 with the message in the list (the polling goes on, and so does the refresh button),
// and goes to the log.
func (h Handlers) list(c *gin.Context) view.List {
	l := view.List{Refresh: paths.JobsList(), UpdatedAt: time.Now().Format(time.TimeOnly)}
	entries, err := h.Svc.Recent(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		l.Error = readFailure
		l.Active = true // a table that cannot be read now may be read in 2 seconds: the polling goes on, and the list comes back by itself
		return l
	}
	for _, e := range entries {
		r := row(e)
		l.Rows = append(l.Rows, r)
		if !finished(e.State) {
			l.Active = true
			l.Running++
		}
	}
	return l
}

func finished(s joblist.State) bool {
	return s == joblist.Succeeded || s == joblist.Failed || s == joblist.Cancelled
}

func row(e joblist.Entry) view.Row {
	r := view.Row{ID: e.ID, Label: e.Label, Subject: e.Subject, StartedAt: e.StartedAt.Local().Format("2006-01-02 15:04"), Error: e.Error, NextSteps: e.NextSteps, Href: href(e.Dest)}
	r.StateText, r.Tone = stateBadge(e.State)
	if e.Progress != nil {
		r.Progress = &view.Progress{Percent: e.Progress.Percent, Text: e.Progress.Text}
	}
	return r
}

func stateBadge(s joblist.State) (string, atoms.BadgeTone) {
	switch s {
	case joblist.Queued:
		return "待機中", atoms.BadgeNeutral
	case joblist.Running:
		return "実行中", atoms.BadgeInfo
	case joblist.Succeeded:
		return "完了", atoms.BadgeOK
	case joblist.Failed:
		return "失敗", atoms.BadgeError
	case joblist.Cancelled:
		return "中止", atoms.BadgeWarn
	}
	return string(s), atoms.BadgeNeutral
}

// href is the screen a job leads to: the model manager for an install, the generation test for a batch, the Project's screens for the
// work on a Shot or the planning of a Project.
func href(d joblist.Dest) string {
	switch d.Screen {
	case joblist.ScreenModelManager:
		return paths.ModelManager()
	case joblist.ScreenGenerationTest:
		return paths.GenerationTestHistory()
	case joblist.ScreenProjectScenes:
		return paths.Project(d.ProjectID, "scenes")
	case joblist.ScreenProjectChat:
		return paths.Project(d.ProjectID)
	}
	return ""
}

func render(c *gin.Context, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}
}
