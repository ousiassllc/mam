// Package live remembers how far the ComfyUI jobs that mam started are, for the tools that show the state of the
// GPU. ComfyUI sends the steps of a job to the WebSocket of the client that queued it only, so no other client can read
// them from the server: the backend (package comfyui) that follows its own jobs records them here. A job another
// client queued (the ComfyUI screen itself) is not known here.
package live

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Progress is how far a running job is.
type Progress struct {
	Endpoint string
	PromptID string
	// Owner is the work that asked for the call (a Shot, a generation test batch), "" when it did not say (see WithOwner).
	Owner string
	// Node is the class of the node that is running (KSampler, ...). A workflow can have several sampling nodes
	// (Wan 2.2 has two), and the steps count inside the current one.
	Node string
	// Step and MaxStep are the step of the current node.
	Step, MaxStep int
	// Percent is how far the whole workflow is, as the nodes done and the steps of the current one tell (0-99).
	Percent   int
	StartedAt time.Time
	// Elapsed is the time since the call was queued (Registry.Start) up to the time the snapshot was taken.
	Elapsed time.Duration
	// ETA is the time the rest of the current node needs at the speed it has had so far; ok is false before it
	// can be told (the node has not advanced yet). It is not the time the whole workflow needs.
	ETA    time.Duration
	HasETA bool
}

// View is Progress as the tools show it (get_gpu_status, get_job, get_generation_test). Step and MaxStep count the steps of
// the node that is running (a workflow can have several sampling nodes: Wan 2.2 has two), Percent the whole workflow.
type View struct {
	Node    string `json:"node,omitempty" jsonschema:"いま動いているノードのクラス（KSampler など）/ class of the node that is running"`
	Step    int    `json:"step" jsonschema:"いまのノードのステップ / step of the current node"`
	MaxStep int    `json:"max_step" jsonschema:"いまのノードのステップの総数 / steps of the current node"`
	Percent int    `json:"percent" jsonschema:"ワークフロー全体の進み具合（0-99 %）/ how far the whole workflow is"`
	// ElapsedSeconds is the time since the call was queued on ComfyUI, the model loading included: it grows even when no step moves.
	ElapsedSeconds int `json:"elapsed_seconds" jsonschema:"ComfyUI に渡してからの経過秒（モデルの読み込みを含む）。ステップが進まなくても増え続ける / seconds since the call was queued on ComfyUI, model loading included"`
	// ETASeconds is the time the rest of the current node needs at the speed it has had; not the time of the whole job.
	ETASeconds     *int   `json:"eta_seconds,omitempty" jsonschema:"いまのノードの残りステップがこれまでの速さで終わるまでの秒数。ワークフロー全体の残り時間ではない / seconds the rest of the current node needs; not the time of the whole job"`
	ETAUnavailable string `json:"eta_unavailable,omitempty" jsonschema:"eta_seconds が無い理由 / why eta_seconds is missing"`
}

// View returns the progress to show, or nil while the server has not reported a step (loading the model, ...). A node whose
// steps are all done has nothing left to estimate: it gets neither an ETA nor a reason for the lack of one.
func (p Progress) View() *View {
	if p.MaxStep == 0 {
		return nil
	}
	v := &View{Node: p.Node, Step: p.Step, MaxStep: p.MaxStep, Percent: p.Percent, ElapsedSeconds: int(p.Elapsed.Round(time.Second) / time.Second)}
	switch {
	case p.HasETA:
		secs := int(p.ETA.Round(time.Second) / time.Second)
		v.ETASeconds = &secs
	case p.Step < p.MaxStep:
		v.ETAUnavailable = "このノードのステップがまだ進んでいないため、残り時間を見積もれません"
	}
	return v
}

type ownerKey struct{}

// WithOwner returns a context whose ComfyUI calls are recorded as the work of owner, so that the tools that show the work
// (get_job, get_generation_test) find its progress with ByOwner. owner is the id of the Shot or of the batch.
func WithOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, ownerKey{}, owner)
}

// OwnedByJob makes the ComfyUI calls of a job handler the work of the job: its progress is found by the id of the queue job
// (the job_id of get_job, BatchStatus.JobID of a generation test).
func OwnedByJob(handle func(context.Context, domain.Job) error) func(context.Context, domain.Job) error {
	return func(ctx context.Context, job domain.Job) error { return handle(WithOwner(ctx, job.ID), job) }
}

// OwnerOf is the owner WithOwner put in ctx, "" if none.
func OwnerOf(ctx context.Context) string {
	owner, _ := ctx.Value(ownerKey{}).(string)
	return owner
}

type key struct{ endpoint, prompt string }

// Registry holds the jobs that are running. The zero value is not usable: use New.
type Registry struct {
	mu   sync.Mutex
	jobs map[key]*Job
	now  func() time.Time
}

// New returns an empty Registry that takes the time from now (nil: time.Now).
func New(now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	return &Registry{jobs: map[key]*Job{}, now: now}
}

// Default is the Registry of the process: the backends record into it and the tools read it.
var Default = New(nil)

// Job is a running job; its methods are safe for concurrent use and a nil Job ignores them.
type Job struct {
	r    *Registry
	key  key
	mu   sync.Mutex
	p    Progress
	node string // id of the node the steps belong to
	// nodeStart is when the current node was first seen, nodeFrom the step it was at then and updated the last message.
	nodeStart, updated time.Time
	nodeFrom           int
}

// Start records a job of the endpoint that the server accepted. End removes it.
func (r *Registry) Start(endpoint, promptID string) *Job {
	j := &Job{r: r, key: key{endpoint, promptID}, p: Progress{Endpoint: endpoint, PromptID: promptID, StartedAt: r.now()}}
	r.mu.Lock()
	r.jobs[j.key] = j
	r.mu.Unlock()
	return j
}

// Step records a progress message: nodeID and class are the node that sends it, value of max its steps, percent the whole workflow.
func (j *Job) Step(nodeID, class string, value, max, percent int) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	now := j.r.now()
	if nodeID != j.node {
		j.node, j.nodeStart, j.nodeFrom = nodeID, now, value
	}
	j.updated = now
	j.p.Node, j.p.Step, j.p.MaxStep, j.p.Percent = class, value, max, percent
}

// Own records whose work the job is (see WithOwner); "" leaves it unowned.
func (j *Job) Own(owner string) {
	if j == nil {
		return
	}
	j.mu.Lock()
	j.p.Owner = owner
	j.mu.Unlock()
}

// End removes the job.
func (j *Job) End() {
	if j == nil {
		return
	}
	j.r.mu.Lock()
	delete(j.r.jobs, j.key)
	j.r.mu.Unlock()
}

// Rename follows the prompt ID the server answered with, when it is not the one the client chose.
func (j *Job) Rename(promptID string) {
	if j == nil {
		return
	}
	j.r.mu.Lock()
	defer j.r.mu.Unlock()
	if j.key.prompt == promptID {
		return
	}
	delete(j.r.jobs, j.key)
	j.key.prompt = promptID
	j.r.jobs[j.key] = j
}

// Lookup returns the progress of a running job of the endpoint.
func (r *Registry) Lookup(endpoint, promptID string) (Progress, bool) {
	r.mu.Lock()
	j, ok := r.jobs[key{endpoint, promptID}]
	r.mu.Unlock()
	if !ok {
		return Progress{}, false
	}
	return j.snapshot(), true
}

// ByOwner returns the progress of the running job of owner; of several (a batch runs a job per endpoint) the one that
// moved last.
func (r *Registry) ByOwner(owner string) (Progress, bool) {
	if owner == "" {
		return Progress{}, false
	}
	r.mu.Lock()
	jobs := make([]*Job, 0, len(r.jobs))
	for _, j := range r.jobs {
		jobs = append(jobs, j)
	}
	r.mu.Unlock()
	var best Progress
	var bestAt time.Time
	found := false
	for _, j := range jobs {
		p, at := j.snapshotAt()
		if p.Owner != owner || (found && !at.After(bestAt)) {
			continue
		}
		best, bestAt, found = p, at, true
	}
	return best, found
}

// snapshotAt is the snapshot and when the job last moved (when it started, before any step).
func (j *Job) snapshotAt() (Progress, time.Time) {
	p := j.snapshot()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.updated.After(p.StartedAt) {
		return p, j.updated
	}
	return p, p.StartedAt
}

func (j *Job) snapshot() Progress {
	j.mu.Lock()
	defer j.mu.Unlock()
	p := j.p
	p.Elapsed = max(0, j.r.now().Sub(p.StartedAt))
	if done, elapsed := p.Step-j.nodeFrom, j.updated.Sub(j.nodeStart); done > 0 && elapsed > 0 && p.MaxStep > p.Step {
		p.ETA, p.HasETA = elapsed*time.Duration(p.MaxStep-p.Step)/time.Duration(done), true
	}
	return p
}
