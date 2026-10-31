package editorstage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
)

// The editing stages (Editor Judge, audio, export) are synchronous in the service layer: the screen
// waits for the request. A tool must answer at once, so a stage runs in the background and the tracker
// holds what it ends with. There is no row in the job table for these stages, so their job_ids are the
// job_<UUIDv7> ones of mcpkit.Jobs. The tracker only lives as long as the process: the end of a stage is
// also recorded in the job history (mcpkit.Jobs.Observe), which answers get_job after a restart.

// earlyFailureWindow is how long a tool waits for a stage that fails at once: the checks that refuse a
// request (wrong state, stage busy, bad option, feature off) run before the long work, so the caller gets
// the error of the screen instead of a job that fails a moment later.
var earlyFailureWindow = 2 * time.Second

// maxRuns is the number of runs the tracker remembers; an older job is answered from the job history.
const maxRuns = 4096

// outcome is what a stage ended with.
type outcome struct {
	done   chan struct{} // closed when the stage ended; err, te and result are set before
	err    error
	te     *mcpkit.ToolError // err as the agent sees it: an internal failure carries its correlation id and was logged with it
	result map[string]any
}

type tracker struct {
	r *mcpkit.Registrar

	mu    sync.Mutex
	runs  map[string]*outcome
	order []string
}

func newTracker(r *mcpkit.Registrar) *tracker { return &tracker{r: r, runs: map[string]*outcome{}} }

// start runs work in the background as a job of kind and target. An error that work returns within
// earlyFailureWindow is returned to the caller and no job is made.
func (t *tracker) start(ctx context.Context, kind, target string, work func(ctx context.Context) (map[string]any, error)) (mcpkit.JobRef, error) {
	o := &outcome{done: make(chan struct{})}
	ref, err := t.r.Jobs.Run(ctx, kind, target, func(ctx context.Context) (string, error) {
		// The stage outlives the tool call; what it stores does not depend on the caller staying.
		bg := context.WithoutCancel(ctx)
		go t.run(bg, kind, target, o, work)
		timer := time.NewTimer(earlyFailureWindow)
		defer timer.Stop()
		select {
		case <-o.done:
			if o.te != nil && o.te.ErrorID != "" {
				return "", o.te // an internal failure: logged already, with the id the caller gets
			}
			return "", o.err
		case <-timer.C:
		case <-ctx.Done():
		}
		return "", nil
	})
	if err != nil {
		return mcpkit.JobRef{}, err
	}
	t.remember(ref.JobID, o)
	// Nobody may ask about the stage before mam stops: the history gets its end now.
	go func() {
		<-o.done
		t.r.Jobs.Observe(context.WithoutCancel(ctx), ref.JobID)
	}()
	return ref, nil
}

func (t *tracker) run(ctx context.Context, kind, target string, o *outcome, work func(ctx context.Context) (map[string]any, error)) {
	defer close(o.done)
	defer func() {
		if p := recover(); p != nil { // a panic here would end mam; a handler's panic is recovered by the router
			o.err = fmt.Errorf("panic in %s: %v", kind, p)
			o.te = mcpkit.ReportError(t.r.Logger, "mcp editing stage panicked", o.err, "kind", kind, "target", target, "panic", p)
		}
	}()
	o.result, o.err = work(ctx)
	if o.err != nil {
		o.te = mcpkit.ReportError(t.r.Logger, "mcp editing stage failed", o.err, "kind", kind, "target", target)
	}
}

func (t *tracker) remember(jobID string, o *outcome) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for len(t.order) >= maxRuns {
		delete(t.runs, t.order[0])
		t.order = t.order[1:]
	}
	t.runs[jobID] = o
	t.order = append(t.order, jobID)
}

func (t *tracker) outcome(jobID string) *outcome {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.runs[jobID]
}

// probe reads a job of a kind from the tracker: running until the stage ended, then failed with the
// error of the tool or succeeded with the result the stage produced.
func (t *tracker) probe(running string) mcpkit.Probe {
	return func(_ context.Context, job mcpkit.JobInfo) (mcpkit.Progress, error) {
		o := t.outcome(job.ID)
		if o == nil {
			return mcpkit.Progress{State: mcpkit.JobFailed, Error: "この job の記録が見つかりません（mam を再起動した可能性があります）。編集画面の状態を get_editor_view で確認してください"}, nil
		}
		select {
		case <-o.done:
		default:
			return mcpkit.Progress{State: mcpkit.JobRunning, Message: running}, nil
		}
		if o.te != nil {
			return mcpkit.Progress{State: mcpkit.JobFailed, Error: o.te.Error(), ErrorID: o.te.ErrorID}, nil
		}
		return mcpkit.Progress{State: mcpkit.JobSucceeded, Result: o.result}, nil
	}
}
