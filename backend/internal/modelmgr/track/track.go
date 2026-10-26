// Package track keeps the progress of the jobs of the model manager (spec/architecture/model-manager/model-manager.md §10). The job
// table has no progress column, so a running job reports here, in memory; the table says whether the job is queued,
// running, succeeded or failed, and what a finished job produced is on disk (the installed files, the trial result).
// A job that is resumed after a restart starts reporting again from its first step.
package track

import (
	"slices"
	"sync"
	"time"
)

// Update is the progress of one step of a job.
type Update struct {
	// Percent is how far the job is, 0-100.
	Percent int `json:"percent"`
	// Message says what it is doing ("qwen_image_vae.safetensors (3/3)").
	Message string `json:"message,omitempty"`
	// Phase is the step: download, extract, verify, register, generate, ...
	Phase string `json:"phase,omitempty"`
	// DoneBytes and TotalBytes are the bytes of a download or extraction (0 when not a transfer).
	DoneBytes  int64 `json:"done_bytes,omitempty"`
	TotalBytes int64 `json:"total_bytes,omitempty"`
}

// Entry is what the tracker knows of a job.
type Entry struct {
	JobID string `json:"job_id"`
	// Target says what the job works on: "engine", "model:<id>" or "gentest:<model id>".
	Target  string    `json:"target"`
	Update  Update    `json:"progress"`
	Updated time.Time `json:"updated_at"`
	Done    bool      `json:"done"`
}

// retain is how many finished entries are kept.
const retain = 128

// Tracker is safe for concurrent use.
type Tracker struct {
	mu      sync.Mutex
	entries map[string]*Entry
	order   []string // job ids, oldest first
	subs    map[int]func(Entry)
	next    int
	now     func() time.Time
}

// New returns an empty Tracker.
func New() *Tracker {
	return &Tracker{entries: map[string]*Entry{}, subs: map[int]func(Entry){}, now: time.Now}
}

// Set records the progress of a job and tells the subscribers.
func (t *Tracker) Set(jobID, target string, u Update) {
	u.Percent = min(100, max(0, u.Percent))
	t.mu.Lock()
	e, ok := t.entries[jobID]
	if !ok {
		e = &Entry{JobID: jobID}
		t.entries[jobID] = e
		t.order = append(t.order, jobID)
		t.trim()
	}
	e.Target, e.Update, e.Updated, e.Done = target, u, t.now().UTC(), false
	t.notifyLocked(*e)
}

// Finish marks the job over; its last progress stays readable.
func (t *Tracker) Finish(jobID string) {
	t.mu.Lock()
	if e, ok := t.entries[jobID]; ok {
		e.Done, e.Updated = true, t.now().UTC()
		t.notifyLocked(*e)
		return
	}
	t.mu.Unlock()
}

// Get is the entry of a job.
func (t *Tracker) Get(jobID string) (Entry, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[jobID]
	if !ok {
		return Entry{}, false
	}
	return *e, true
}

// Subscribe calls fn with every change (from the goroutine that changed it; fn must not block or call back
// into the Tracker) until the returned function is called.
func (t *Tracker) Subscribe(fn func(Entry)) (cancel func()) {
	t.mu.Lock()
	id := t.next
	t.next++
	t.subs[id] = fn
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		delete(t.subs, id)
		t.mu.Unlock()
	}
}

// notifyLocked releases the lock before it calls the subscribers.
func (t *Tracker) notifyLocked(e Entry) {
	fns := make([]func(Entry), 0, len(t.subs))
	for _, fn := range t.subs {
		fns = append(fns, fn)
	}
	t.mu.Unlock()
	for _, fn := range fns {
		fn(e)
	}
}

// trim drops the oldest finished entries beyond retain.
func (t *Tracker) trim() {
	for len(t.order) > retain {
		i := slices.IndexFunc(t.order, func(id string) bool { return t.entries[id].Done })
		if i < 0 {
			return
		}
		delete(t.entries, t.order[i])
		t.order = slices.Delete(t.order, i, i+1)
	}
}
