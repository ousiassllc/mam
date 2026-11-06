// Package instlog is what the install jobs of the model manager tell about their downloads: the log lines (with the job and the
// model, so that a failure can be found in the app log).
package instlog

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// jobKey carries the install job to the retry hook of the downloads, which has no other way to know it.
type jobKey struct{}

type job struct{ id, target string }

// WithJob returns ctx marked as the context of the install job id, which works on target ("engine", "model:<id>", "hf:…").
func WithJob(ctx context.Context, id, target string) context.Context {
	return context.WithValue(ctx, jobKey{}, job{id, target})
}

// RetryLogger is the OnRetry of the downloads: it logs every resume (WARN) with the job and the model of its context (WithJob),
// then calls next (the hook that was set before; nil for none).
func RetryLogger(l *slog.Logger, next func(context.Context, fetch.RetryEvent)) func(context.Context, fetch.RetryEvent) {
	return func(ctx context.Context, e fetch.RetryEvent) {
		j, _ := ctx.Value(jobKey{}).(job)
		fetch.LogRetry(l, e, "job_id", j.id, "target", j.target)
		if next != nil {
			next(ctx, e)
		}
	}
}

// Finished logs how the install job ended, with the job and the model (target): INFO for a success and for a stop the user asked
// for (userStop), WARN for an interruption by the shutdown, ERROR for a failure (with the cause, not the message the user reads).
func Finished(ctx context.Context, l *slog.Logger, jobID, target string, err error, userStop bool) {
	switch {
	case err == nil:
		l.Info("install succeeded", "job_id", jobID, "target", target)
	case userStop:
		l.Info("install cancelled by the user", "job_id", jobID, "target", target)
	case ctx.Err() != nil:
		l.Warn("install interrupted", "job_id", jobID, "target", target, "err", err)
	default:
		l.Error("install failed", "job_id", jobID, "target", target, "err", err)
	}
}
