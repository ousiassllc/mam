// Package live remembers how far the ComfyUI jobs that mam started are, for the tools that show the state of the
// GPU. ComfyUI sends the steps of a job to the WebSocket of the client that queued it only, so no other client can read
// them from the server: the backend (package comfyui) that follows its own jobs records them here. A job another
// client queued (the ComfyUI screen itself) is not known here.
package live

import (
	"sync"
	"time"
)

// Progress is how far a running job is.
type Progress struct {
	Endpoint string
	PromptID string
	// Node is the class of the node that is running (KSampler, ...). A workflow can have several sampling nodes
	// (Wan 2.2 has two), and the steps count inside the current one.
	Node string
	// Step and MaxStep are the step of the current node.
	Step, MaxStep int
	// Percent is how far the whole workflow is, as the nodes done and the steps of the current one tell (0-99).
	Percent   int
	StartedAt time.Time
	// ETA is the time the rest of the current node needs at the speed it has had so far; ok is false before it
	// can be told (the node has not advanced yet). It is not the time the whole workflow needs.
	ETA    time.Duration
	HasETA bool
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

func (j *Job) snapshot() Progress {
	j.mu.Lock()
	defer j.mu.Unlock()
	p := j.p
	if done, elapsed := p.Step-j.nodeFrom, j.updated.Sub(j.nodeStart); done > 0 && elapsed > 0 && p.MaxStep > p.Step {
		p.ETA, p.HasETA = elapsed*time.Duration(p.MaxStep-p.Step)/time.Duration(done), true
	}
	return p
}
