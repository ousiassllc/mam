package exeinstall

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// RetryLogger is the OnRetry of the download of the program called target ("ffmpeg", "codex"): it logs every automatic resume
// (WARN, fetch.LogRetry), then calls next (the hook that was set before; nil for none).
func RetryLogger(l *slog.Logger, target string, next func(context.Context, fetch.RetryEvent)) func(context.Context, fetch.RetryEvent) {
	return func(ctx context.Context, e fetch.RetryEvent) {
		fetch.LogRetry(l, e, "target", target)
		if next != nil {
			next(ctx, e)
		}
	}
}
