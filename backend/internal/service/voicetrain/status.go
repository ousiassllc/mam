package voicetrain

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// Disk (mam's conservative estimate, spec §16.3: not measured). The working directory takes the checkpoints of the two trainings (they hold the
// optimizer, and the GPT keeps the weights of two epochs), the 32 kHz copies and the features of the clips; the weights that are kept are about
// 250 MB (the base weights, half precision), and are written to the assets directory once the training is over.
const (
	reserveBytes     = 6 << 30
	perSecondBytes   = 200 << 10
	keptWeightsBytes = 512 << 20
)

// Status is one training: where it stands and how far it is. A training that ended keeps its Error (the words) and Failure (the common error), and
// the trained voice it made.
type Status struct {
	ID          string
	State       worktrack.State
	CharacterID string
	// Character is the name of the Character that is trained.
	Character string
	// Progress is how far it is; Text says it in words and Percent is Progress.Percent (what the job list shows).
	Progress Progress
	Text     string
	Epochs   Options
	// Error is why a training that failed failed, in words; Failure is the same as the common error.
	Error      string
	Failure    *apperr.Error
	Trained    *voiceref.Finetune
	StartedAt  time.Time
	FinishedAt *time.Time
}

// Running reports whether the training is still going on.
func (s Status) Running() bool { return s.State == worktrack.Running }

type run struct {
	id    string
	key   key
	owner voicerefsvc.Owner
	work  *worktrack.Run

	cancel context.CancelCauseFunc
	done   chan struct{}

	mu      sync.Mutex
	status  Status
	stage   int
	count   [2]int // done and total counted by the stage
	stageAt time.Time
}

// snapshot is the Status with the times of this moment.
func (r *run) snapshot(now time.Time) Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.status
	if st.Running() {
		st.Progress = at(r.stage, r.count[0], r.count[1], now.Sub(st.StartedAt), now.Sub(r.stageAt))
		st.Text = st.Progress.Text()
	}
	return st
}

// publish tells the job list how far the training is now.
func (r *run) publish(now time.Time) {
	st := r.snapshot(now)
	if st.Running() {
		r.work.Progress(st.Progress.Percent, st.Text)
	}
}

func (r *run) begin(i int, now time.Time) {
	r.mu.Lock()
	r.stage, r.count, r.stageAt = i, [2]int{}, now
	r.mu.Unlock()
	r.publish(now)
}

func (r *run) counted(done, total int, now time.Time) {
	r.mu.Lock()
	r.count = [2]int{done, total}
	r.mu.Unlock()
	r.publish(now)
}

// finish ends the run in the state with the reason, the failure and the voice it made.
func (r *run) finish(state worktrack.State, err error, failure *apperr.Error, trained *voiceref.Finetune, now time.Time) {
	r.mu.Lock()
	r.status.State, r.status.Failure, r.status.Trained, r.status.FinishedAt = state, failure, trained, &now
	if err != nil {
		r.status.Error = err.Error()
		if failure != nil {
			r.status.Error = failure.View(apperr.LangJa).Sentence(apperr.LangJa)
		}
	}
	r.mu.Unlock()
	switch state {
	case worktrack.Succeeded:
		r.work.Succeed()
	case worktrack.Cancelled:
		r.work.Cancel()
	default:
		r.work.Fail(r.snapshot(now).Error)
	}
}
