package gentest

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/history"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/exportbar"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// fieldCommercial is the query field of the commercial-use filter of the history.
const fieldCommercial = "commercial"

// historyPage is how many results one page of the history lists.
const historyPage = 24

// liveParam ("?live=1") asks for the part of the history that changes while something runs (view.HistoryLive), not the whole list.
const liveParam = "live"

// historyQuery is the query of a page of the history: the filter and the offset ("" and 0 are left out).
func historyQuery(f view.HistoryFilter, offset int) url.Values {
	q := url.Values{}
	for k, v := range map[string]string{view.FieldModel: f.Model, view.FieldMedia: f.Media} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if f.Favorite {
		q.Set("favorite", "1")
	}
	if f.Commercial != "" {
		q.Set(fieldCommercial, f.Commercial)
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	return q
}

// history builds the history list of the filter in the query ("?model=&media=&favorite=1&offset=").
func (h Handlers) history(ctx context.Context, q url.Values) (view.History, error) {
	f := view.HistoryFilter{Model: q.Get(view.FieldModel), Media: q.Get(view.FieldMedia), Favorite: q.Get("favorite") == "1", Commercial: q.Get(fieldCommercial)}
	offset, _ := strconv.Atoi(q.Get("offset"))
	offset = max(offset, 0)
	chosen := h.Svc.DefaultFilter()
	if parsed, ok := modellicense.ParseFilter(f.Commercial); ok && f.Commercial != "" {
		chosen = parsed
	}
	live := historyQuery(f, offset)
	live.Set(liveParam, "1")
	out := view.History{
		Filter: f, Commercial: commercial.NewFilter("gt-history-commercial", "#"+view.HistoryID, chosen).WithField(fieldCommercial),
		Refresh: paths.GenerationTestHistory() + "?" + live.Encode(), UpdatedAt: time.Now().Format(time.TimeOnly),
	}
	out.Export = exportbar.Data{URL: paths.GenerationTestExport(), Model: f.Model, Media: f.Media, Favorite: f.Favorite, Commercial: string(chosen)}
	list, err := h.Svc.History(ctx, history.Filter{Model: f.Model, Media: f.Media, FavoriteOnly: f.Favorite, Limit: historyPage + 1, Offset: offset})
	if err != nil {
		return out, err
	}
	if len(list) > historyPage {
		list = list[:historyPage]
		out.Next = paths.GenerationTestHistory() + "?" + historyQuery(f, offset+historyPage).Encode()
	}
	for _, r := range list {
		out.Cards = append(out.Cards, cardOf(r, true))
		out.Active = out.Active || r.Status == gentest.StatusQueued || r.Status == gentest.StatusRunning
	}
	if out.Models, err = h.Svc.HistoryModels(ctx); err != nil {
		return out, err
	}
	out.ModelAnswers = make(map[string]string, len(out.Models))
	for _, m := range out.Models {
		out.ModelAnswers[m] = commercial.Of(m).Answer()
	}
	s, err := h.Svc.Spend(ctx)
	out.Spend = spendText(s)
	return out, err
}

// History answers GET /generation-test/history (the page, or the list alone for an HTMX request): newest first, filtered by
// model, media and favorite.
func (h Handlers) History(c *gin.Context) {
	v, err := h.history(c.Request.Context(), c.Request.URL.Query())
	status := http.StatusOK
	if err != nil {
		status, v.Error = refuse(c, err)
	}
	if htmxFragment(c) {
		if c.Query(liveParam) == "1" {
			gtshare.Render(c, status, view.HistoryLive(v))
			return
		}
		gtshare.Render(c, status, view.HistoryList(v))
		return
	}
	gtshare.Render(c, status, pages.GenerationHistoryPage(middleware.CSRFToken(c), v))
}

// Detail answers GET /generation-test/results/:id: the settings, the inputs, the imports and the import form.
func (h Handlers) Detail(c *gin.Context) {
	ctx := c.Request.Context()
	d, err := h.Svc.Result(ctx, c.Param("id"))
	if err != nil {
		status, msg := refuse(c, err)
		gtshare.Render(c, status, pages.GenerationResultPage(middleware.CSRFToken(c), view.Detail{Message: view.Message{Error: msg}}))
		return
	}
	gtshare.Render(c, http.StatusOK, pages.GenerationResultPage(middleware.CSRFToken(c), h.detail(ctx, d, view.ImportForm{})))
}

// serveFile answers with a stored file as the media routes do: the stored type, inside a sandbox so that the file cannot run
// as a page of mam; "?download=1" asks the browser to save it.
func serveFile(c *gin.Context, f history.File, err error) {
	if err != nil {
		if c.Request.Context().Err() != nil {
			return
		}
		if gterrNotFound(err) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		_ = c.Error(err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Header("Content-Type", f.Mime)
	c.Header("Content-Security-Policy", "sandbox")
	c.Header("X-Content-Type-Options", "nosniff")
	if c.Query("download") == "1" {
		c.Header("Content-Disposition", `attachment; filename="`+f.Name+`"`)
	}
	c.File(f.Path)
}

func gterrNotFound(err error) bool { return errors.Is(err, gterr.ErrNotFound) }

// ResultFile answers GET /generation-test/results/:id/file.
func (h Handlers) ResultFile(c *gin.Context) {
	f, err := h.Svc.ResultFile(c.Request.Context(), c.Param("id"))
	serveFile(c, f, err)
}

// InputFile answers GET /generation-test/inputs/:id/file: an input image of a batch.
func (h Handlers) InputFile(c *gin.Context) {
	f, err := h.Svc.InputFile(c.Request.Context(), c.Param("id"))
	serveFile(c, f, err)
}

// Favorite answers POST /generation-test/results/:id/favorite ("favorite=1" sets, "0" clears) with the button as it is now.
func (h Handlers) Favorite(c *gin.Context) {
	on := c.PostForm("favorite") == "1"
	if err := h.Svc.SetFavorite(c.Request.Context(), c.Param("id"), on); err != nil {
		status, _ := refuse(c, err)
		c.AbortWithStatus(status)
		return
	}
	gtshare.Render(c, http.StatusOK, view.FavoriteButton(c.Param("id"), on))
}

// DeleteResult answers DELETE /generation-test/results/:id: the result and its file go; the history is shown again. A result
// that has not finished is 409.
func (h Handlers) DeleteResult(c *gin.Context) {
	if err := h.Svc.DeleteResult(c.Request.Context(), c.Param("id")); err != nil {
		status, msg := refuse(c, err)
		c.Header("HX-Retarget", "#"+view.DetailID)
		c.Header("HX-Reswap", "outerHTML")
		gtshare.Render(c, status, view.ResultDetail(view.Detail{Message: view.Message{Error: msg}}))
		return
	}
	c.Header("HX-Redirect", paths.GenerationTestHistory())
	c.Status(http.StatusOK)
}

// Prompts answers GET /generation-test/prompts: the prompt history.
func (h Handlers) Prompts(c *gin.Context) {
	gtshare.Render(c, http.StatusOK, pages.GenerationPromptsPage(middleware.CSRFToken(c), h.prompts(c.Request.Context(), view.Message{})))
}

func (h Handlers) prompts(ctx context.Context, m view.Message) view.Prompts {
	p := view.Prompts{Message: m}
	list, err := h.Svc.Prompts(ctx)
	if err != nil {
		p.Error = "プロンプトの履歴を読み込めませんでした。mam のログを確認してください"
		return p
	}
	for _, e := range list {
		p.Items = append(p.Items, view.PromptRow{
			ID: e.ID, Text: e.Prompt, Negative: e.NegativePrompt, Uses: e.UseCount, LastUsed: timeText(e.LastUsedAt),
			UseURL: paths.GenerationTest() + "?prompt=" + url.QueryEscape(e.ID),
		})
	}
	return p
}

// DeletePrompt answers DELETE /generation-test/prompts/:id: only the prompt goes; the results made with it stay.
func (h Handlers) DeletePrompt(c *gin.Context) {
	err := h.Svc.DeletePrompt(c.Request.Context(), c.Param("id"))
	m := view.Message{Notice: "プロンプトを履歴から削除しました（結果は残っています）"}
	status := http.StatusOK
	if err != nil {
		status, m.Error = refuse(c, err)
		m.Notice = ""
	}
	gtshare.Render(c, status, view.PromptHistory(h.prompts(c.Request.Context(), m)))
}
