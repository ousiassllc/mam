// Package toolstatus answers GET /tools/<tool>/status: the status of the tool cards of the home page
// (spec/components/home-screen.md §5). The page shows the cards at once and each card reads its status after that
// (hx-get, hx-trigger="load"), so a slow source never delays the page. Everything is read from this machine (the
// database, the settings files, the key store, the state of the bundled ComfyUI); nothing calls an external API.
// It is a package of its own because handler is at its size limit; like handler it depends on the service layer and the
// web layer only.
//
// The answer is always 200 with a fragment of badges, or nothing when the status cannot be read: a failing card must
// not raise the error toast once for each card, and the card is still a link.
package toolstatus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Deadline is how long a status may take before the card is left empty.
const Deadline = 2 * time.Second

// Source reads the lines of one card. A nil source (the service behind it is not in this router) or an error leaves the
// card without a status.
type Source func(ctx context.Context) ([]molecules.StatusLine, error)

// Handlers answer the routes of the cards that have a status.
type Handlers struct {
	// Sources are keyed by the tool of the card (paths.ToolNodes ...).
	Sources map[string]Source
	// Timeout is the deadline of a read (Deadline when zero).
	Timeout time.Duration
	// Logger gets the failed, late and panicking reads at warn level (nil: discarded). The answer is an empty 200 either way.
	Logger *slog.Logger
}

// Register adds GET paths.ToolStatus(tool) for each of the tools of the home page. A tool of another name has no
// route: the router answers 404.
func Register(engine *gin.Engine, h Handlers) {
	if h.Logger == nil {
		h.Logger = slog.New(slog.DiscardHandler)
	}
	for _, tool := range []string{paths.ToolNodes, paths.ToolWorkflows, paths.ToolModelManager, paths.ToolSettings, paths.ToolReviews} {
		engine.GET(paths.ToolStatus(tool), h.status(tool, h.Sources[tool]))
	}
}

func (h Handlers) status(tool string, src Source) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		timeout := h.Timeout
		if timeout == 0 {
			timeout = Deadline
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		render(c, molecules.ToolStatus(read(ctx, h.Logger.With("tool", tool), src)))
	}
}

type result struct {
	lines []molecules.StatusLine
	err   error
}

// read runs src and gives up at the deadline: some sources take no context, so the read runs on its own goroutine
// (its answer goes into a buffered channel and is dropped when nobody waits any more). An error, a panic and a missed
// deadline are logged and leave the card empty.
func read(ctx context.Context, logger *slog.Logger, src Source) []molecules.StatusLine {
	if src == nil {
		return nil
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if v := recover(); v != nil { // not under the recovery middleware: a panic here would end the process
				done <- result{err: fmt.Errorf("panic: %v\n%s", v, debug.Stack())}
			}
		}()
		lines, err := src(ctx)
		done <- result{lines, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			logger.Warn("tool status: source failed", "error", r.err)
			return nil
		}
		return r.lines
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) { // a canceled request is a reader that left, not a fault
			logger.Warn("tool status: source missed the deadline", "error", ctx.Err())
		}
		return nil
	}
}

func render(c *gin.Context, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
	}
}
