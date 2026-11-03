// Package worktrack is the in-memory record of the work that has no row in the job table (spec/components/jobs-screen.md §2): the
// export of a video, the audio stage, the Editor Judge, the run of a workflow, the install of Codex and the download of ffmpeg.
// The owner of such a work calls Begin when it starts, Run.Progress while it goes on and one of Run.Succeed, Run.Fail, Run.Cancel
// or Run.End when it ends; the job list reads Tracker.Works. Nothing is persisted: a restart of the process ends the record, as it ends
// the work (none of them is resumable).
//
// It is a leaf package (it imports domain only) so that every owner, from the media layer to the services, can use it.
package worktrack

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Kind is the kind of a tracked work.
type Kind string

// The kinds of work that are tracked in memory. None of them is a domain.JobKind: the job table has no row for them.
const (
	KindExport        Kind = "export"         // the finished video of a Project
	KindAudio         Kind = "audio"          // the audio stage (voices, BGM, effects and the mix) of a Project
	KindEditorJudge   Kind = "editor_judge"   // the Editor Judge of an Edit Timeline
	KindWorkflowRun   Kind = "workflow_run"   // one run of a saved ComfyUI workflow
	KindCodexInstall  Kind = "codex_install"  // the download of the Codex app-server
	KindFFmpegInstall Kind = "ffmpeg_install" // the download of ffmpeg and ffprobe
	KindVoiceTrain    Kind = "voice_train"    // the fine-tuning of a Character's voice (GPT-SoVITS; Issue #936)
)

// State is where a work stands. A tracked work starts running at once: nothing here waits in a queue.
type State string

// The states of a work.
const (
	Running   State = "running"
	Succeeded State = "succeeded"
	Failed    State = "failed"
	Cancelled State = "cancelled"
)

// NoPercent is the Percent of a work that cannot say how far it is (an ffmpeg render, a model that reasons): the list shows its
// words and the time it has run, no bar.
const NoPercent = -1

// Record is one work.
type Record struct {
	ID   string
	Kind Kind
	// ProjectID is the Project a work of the editing half is on (the list names it and leads to its editor); "" for the others.
	ProjectID string
	// SeriesID is the Series a work on a canonical Character is on (KindVoiceTrain: the screen of the canon); "" for the others.
	SeriesID string
	// Subject is what the work is on when the owner knows its name (the workflow of a run, "ffmpeg"). ModelID is the model whose
	// adult-ness hides the Subject while the age confirmation is off (a workflow run); "" when none.
	Subject string
	ModelID string
	State   State
	// StartedAt is when it began; FinishedAt when it ended (nil while it runs).
	StartedAt  time.Time
	FinishedAt *time.Time
	// Percent is 0-100, or NoPercent. Text says in words what it is doing. Both are of a running work.
	Percent int
	Text    string
	// Error is why a failed work failed, as the owner words it for the user.
	Error string
}

// Source is what the job list reads: the works of an owner, the running ones and the latest that ended.
type Source interface {
	Works() []Record
}

// Keep is how many ended works a Tracker remembers (the running ones are always kept).
const Keep = 20

// Tracker holds the works of one owner. The zero value is ready to use.
type Tracker struct {
	mu   sync.Mutex
	recs []*Record // oldest first
	// Now gives the time of Begin and End; nil is time.Now. Tests set it.
	Now func() time.Time
}

// Run is a running work: how its owner reports on it. Every method is safe to call from any goroutine and after the work ended
// (it is then ignored), and on a nil Run (a work whose owner has no tracker).
type Run struct {
	t   *Tracker
	rec *Record
}

func (t *Tracker) now() time.Time {
	if t.Now != nil {
		return t.Now().UTC()
	}
	return time.Now().UTC()
}

// Begin records a work as running. Kind, ProjectID, Subject and ModelID of r are kept, and so is its ID when the owner has one (a
// workflow run keeps the ID of its record); the rest is set here (a new ID otherwise, the Percent is NoPercent until Progress says
// one). text is what the work is doing at the start.
func (t *Tracker) Begin(r Record, text string) *Run {
	if r.ID == "" {
		r.ID = domain.NewID()
	}
	r.State, r.StartedAt, r.FinishedAt, r.Percent, r.Text, r.Error = Running, t.now(), nil, NoPercent, text, ""
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recs = append(t.recs, &r)
	t.trim()
	return &Run{t: t, rec: &r}
}

// trim drops the oldest ended works beyond Keep.
func (t *Tracker) trim() {
	ended := 0
	for _, r := range t.recs {
		if r.State != Running {
			ended++
		}
	}
	kept := t.recs[:0]
	for _, r := range t.recs {
		if r.State != Running && ended > Keep {
			ended--
			continue
		}
		kept = append(kept, r)
	}
	clear(t.recs[len(kept):])
	t.recs = kept
}

// Works lists the works, the newest start first (the order of the list).
func (t *Tracker) Works() []Record {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Record, len(t.recs))
	for i, r := range t.recs {
		out[len(t.recs)-1-i] = *r
	}
	return out
}

// Progress says how far the work is (percent: 0-100 or NoPercent) and what it is doing.
func (r *Run) Progress(percent int, text string) {
	r.update(func(rec *Record) { rec.Percent, rec.Text = percent, text })
}

// Text says what the work is doing, keeping the percentage.
func (r *Run) Text(text string) {
	r.update(func(rec *Record) { rec.Text = text })
}

// Succeed ends the work as done.
func (r *Run) Succeed() { r.end(Succeeded, "") }

// Fail ends the work as failed for the reason (words for the user).
func (r *Run) Fail(reason string) { r.end(Failed, reason) }

// Cancel ends the work as stopped by the user: not a failure.
func (r *Run) Cancel() { r.end(Cancelled, "") }

// End ends the work by the error its owner got: none is a success, a cancelled context is a cancel, anything else a failure.
func (r *Run) End(err error) {
	switch {
	case err == nil:
		r.Succeed()
	case errors.Is(err, context.Canceled):
		r.Cancel()
	default:
		r.Fail(err.Error())
	}
}

func (r *Run) update(f func(*Record)) {
	if r == nil {
		return
	}
	r.t.mu.Lock()
	defer r.t.mu.Unlock()
	if r.rec.State == Running {
		f(r.rec)
	}
}

func (r *Run) end(state State, reason string) {
	if r == nil {
		return
	}
	r.t.mu.Lock()
	defer r.t.mu.Unlock()
	if r.rec.State != Running {
		return
	}
	now := r.t.now()
	r.rec.State, r.rec.FinishedAt, r.rec.Error, r.rec.Percent, r.rec.Text = state, &now, reason, NoPercent, ""
	r.t.trim()
}
