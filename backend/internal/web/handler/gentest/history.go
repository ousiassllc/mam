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
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/redo"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/pagination"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/exportbar"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/importbox"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/resulttile"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// fieldCommercial is the query field of the commercial-use filter of the history.
const fieldCommercial = "commercial"

// historyPage is how many results one page of the history lists.
const historyPage = 24

// pageParam ("?page=3", from 1) is the page of the history.
const pageParam = "page"

// liveParam ("?live=1") asks for the part of the history that changes while something runs (view.HistoryLive), not the whole list.
const liveParam = "live"

// historyQuery is the query of a page of the history: the filter and the page ("" and the first page are left out).
func historyQuery(f view.HistoryFilter, page int) url.Values {
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
	if page > 1 {
		q.Set(pageParam, strconv.Itoa(page))
	}
	return q
}

// historyURL is the address of a page of the history under the filter.
func historyURL(f view.HistoryFilter, page int) string {
	u := paths.GenerationTestHistory()
	if q := historyQuery(f, page); len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// pageOf reads the page of the query ("?page=3", from 1): a value that is missing, not a number or below 1 is the first page. A page past the last one is
// rounded down to the last by the caller, which knows how many there are.
func pageOf(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 1
	}
	return max(n, 1)
}

// lastPage is the number of pages of total results (1 when there are none: the empty list is the first page).
func lastPage(total int) int { return max(1, (total+historyPage-1)/historyPage) }

// history builds the history list of the filter in the query ("?model=&media=&favorite=1&page="). A page past the last one is the last one (a delete that
// emptied the last page shows the one before it), and the part that refreshes asks for the page that is shown.
func (h Handlers) history(ctx context.Context, q url.Values) (view.History, error) {
	f := view.HistoryFilter{Model: q.Get(view.FieldModel), Media: q.Get(view.FieldMedia), Favorite: q.Get("favorite") == "1", Commercial: q.Get(fieldCommercial)}
	chosen := h.Svc.DefaultFilter()
	if parsed, ok := modellicense.ParseFilter(f.Commercial); ok && f.Commercial != "" {
		chosen = parsed
	}
	page := pageOf(q.Get(pageParam))
	out := view.History{Filter: f, Commercial: commercial.NewChoice("gt-history-commercial", fieldCommercial, chosen), UpdatedAt: time.Now().Format(time.TimeOnly)}
	out.Refresh = refreshURL(f, page)
	out.Export = exportbar.Data{URL: paths.GenerationTestExport(), Model: f.Model, Media: f.Media, Favorite: f.Favorite, Commercial: string(chosen)}
	var err error
	if out.Models, err = h.Svc.HistoryModels(ctx); err != nil {
		return out, err
	}
	// The commercial choice narrows by the models whose answer it allows (a result carries the answer of the model that made it).
	allowed, narrowed := commercialModels(out.Models, chosen)
	var list []record.ResultView
	total := 0
	if !narrowed || len(allowed) > 0 {
		filter := history.Filter{Model: f.Model, Models: allowed, Media: f.Media, FavoriteOnly: f.Favorite}
		if total, err = h.Svc.CountHistory(ctx, filter); err != nil {
			return out, err
		}
		page = min(page, lastPage(total))
		out.Refresh = refreshURL(f, page)
		filter.Limit, filter.Offset = historyPage, (page-1)*historyPage
		if list, err = h.Svc.History(ctx, filter); err != nil {
			return out, err
		}
	}
	out.Pager = pagination.Props{
		ID: "gt-pager", Target: view.HistoryID, Current: page, Last: lastPage(total), Total: total, PerPage: historyPage,
		Href: func(n int) string { return historyURL(f, n) },
	}
	for _, r := range list {
		out.Cards = append(out.Cards, cardOf(r, true))
		out.Active = out.Active || r.Status == gentest.StatusQueued || r.Status == gentest.StatusRunning
	}
	s, err := h.Svc.Spend(ctx)
	out.Spend = spendText(s)
	return out, err
}

// refreshURL is the address of the part of the history that refreshes: the page of the filter as it is now, with "live=1".
func refreshURL(f view.HistoryFilter, page int) string {
	q := historyQuery(f, page)
	q.Set(liveParam, "1")
	return paths.GenerationTestHistory() + "?" + q.Encode()
}

// commercialModels are the models of the history whose answer the filter allows; narrowed says the filter leaves some out (so that no model at all allowed is
// an empty history, not an unfiltered one).
func commercialModels(models []string, f modellicense.Filter) (allowed []string, narrowed bool) {
	if f == modellicense.FilterAll {
		return nil, false
	}
	for _, m := range models {
		if f.Allows(commercial.Of(m).Level) {
			allowed = append(allowed, m)
		}
	}
	return allowed, true
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

// Detail answers GET /generation-test/results/:id: the settings, the inputs, the imports and the import form. A tile of a grid asks for it into the modal
// (HX-Target: the body of the dialog; Issue #1004) and gets the same detail as the body of the modal; anything else gets the page.
func (h Handlers) Detail(c *gin.Context) {
	ctx := c.Request.Context()
	inModal := forModal(c)
	d, err := h.Svc.Result(ctx, c.Param("id"))
	if err != nil {
		status, msg := refuse(c, err)
		if inModal {
			gtshare.Render(c, status, modal.Notice("生成結果", msg))
			return
		}
		gtshare.Render(c, status, pages.GenerationResultPage(middleware.CSRFToken(c), view.Detail{Message: view.Message{Error: msg}}))
		return
	}
	v := h.detail(ctx, d, importbox.Form{})
	v.Upscale.Scale, _ = strconv.Atoi(c.Query(redo.ParamUpscaleScale)) // "同じ倍率でもう一度大きくする" of an enlargement
	if v.Upscale.Show {
		v.Upscale.HasModel, v.Upscale.Model = h.upscaleModel(ctx, c.Query(redo.ParamUpscaleModel))
	}
	if inModal {
		v.CanPick = c.Query("pick") == "1"
		gtshare.Render(c, http.StatusOK, view.ResultDetailModal(v))
		return
	}
	gtshare.Render(c, http.StatusOK, pages.GenerationResultPage(middleware.CSRFToken(c), v))
}

// forModal says the request is from a control that opens the modal: its answer goes into the body of the dialog. The answer differs by what asked, so the
// cache is told.
func forModal(c *gin.Context) bool {
	c.Writer.Header().Add("Vary", "HX-Request, HX-Target")
	return c.GetHeader("HX-Request") == "true" && c.GetHeader("HX-Target") == modal.BodyID
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
	gtshare.Render(c, http.StatusOK, resulttile.Reply(c.Param("id"), on))
}

// DeleteResult answers DELETE /generation-test/results/:id: the result and its file go; the history is shown again. A result
// that has not finished is 409 (the detail is drawn again with the reason). Asked from the modal, the answer takes the result out of the grid behind
// it and closes the modal; asked from the page of the result, it goes to the history.
func (h Handlers) DeleteResult(c *gin.Context) {
	ctx := c.Request.Context()
	inModal := forModal(c)
	if err := h.Svc.DeleteResult(ctx, c.Param("id")); err != nil {
		status, msg := refuse(c, err)
		v := view.Detail{}
		if d, derr := h.Svc.Result(ctx, c.Param("id")); derr == nil {
			v = h.detail(ctx, d, importbox.Form{}) // the reason on top of the detail: it is not wiped
		}
		v.Message = view.Message{Error: msg}
		c.Header("HX-Retarget", "#"+view.DetailID)
		c.Header("HX-Reswap", "outerHTML")
		gtshare.Render(c, status, view.DetailSection(v, inModal))
		return
	}
	if inModal {
		c.Header("HX-Trigger-After-Swap", modal.CloseEvent)
		gtshare.Render(c, http.StatusOK, resulttile.Gone(c.Param("id")))
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
