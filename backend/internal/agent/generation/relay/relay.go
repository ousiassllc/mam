// Package relay turns what a model backend reports about a call in flight (progress.Report: a local model
// such as ComfyUI knows how far its workflow is) into the Shot progress notifications of the Generation
// Orchestrator. The notification keeps what the Orchestrator last said about the Shot (the action, the
// Takes done) and adds the percentage to its message: "動画を生成しています（42%）".
package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/progress"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Relay forwards notifications to next and adds the progress of the model calls.
type Relay struct {
	next func(generation.Progress)
	mu   sync.Mutex
	last map[string]generation.Progress // by Shot ID
}

// New returns a Relay that publishes to next.
func New(next func(generation.Progress)) *Relay {
	return &Relay{next: next, last: map[string]generation.Progress{}}
}

// Notify is the Orchestrator's Deps.Notify.
func (r *Relay) Notify(p generation.Progress) {
	r.mu.Lock()
	if p.Stage == generation.StageGenerating {
		r.last[p.ShotID] = p
	} else {
		delete(r.last, p.ShotID)
	}
	r.mu.Unlock()
	r.next(p)
}

// Wrap returns the job handler that reports the model calls of the job's Shot through the Relay.
func (r *Relay) Wrap(handle func(context.Context, domain.Job) error) func(context.Context, domain.Job) error {
	return func(ctx context.Context, job domain.Job) error {
		var p struct {
			ShotID string            `json:"shot_id"`
			Action generation.Action `json:"action"`
		}
		if json.Unmarshal(job.Payload, &p) == nil && p.ShotID != "" {
			ctx = progress.With(ctx, func(u progress.Update) { r.report(p.ShotID, p.Action, u) })
		}
		return handle(ctx, job)
	}
}

func (r *Relay) report(shotID string, action generation.Action, u progress.Update) {
	r.mu.Lock()
	cur, ok := r.last[shotID]
	if !ok {
		cur = generation.Progress{ShotID: shotID, Action: action, Stage: generation.StageGenerating, Message: "生成しています"}
		r.last[shotID] = cur
	}
	r.mu.Unlock()
	cur.Message = fmt.Sprintf("%s（%d%%）", cur.Message, u.Percent)
	r.next(cur)
}
