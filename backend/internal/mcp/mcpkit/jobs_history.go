package mcpkit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobhistory"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// JobHistory is the persisted history of the jobs (repository/jobhistory.Repository). Every job_id Run hands
// out has a row in it, so that Get still knows the job after a restart.
type JobHistory interface {
	Insert(ctx context.Context, e jobhistory.Entry) error
	Get(ctx context.Context, id string) (jobhistory.Entry, error)
	Settle(ctx context.Context, e jobhistory.Entry) error
	Open(ctx context.Context, limit int) ([]jobhistory.Entry, error)
	List(ctx context.Context, f jobhistory.Filter) (jobhistory.Page, error)
}

// lostMessage is what a job says when mam restarted while it ran and nothing recorded its end.
const lostMessage = "mam が再起動したため、この操作の結果を確かめられません（途中で終わった可能性があります）。対象の状態を画面か該当の get_* ツールで確認してください"

// maxRefresh bounds how many unfinished jobs one listing looks at again.
const maxRefresh = 200

// statusOf answers for a job of the history: what its Probe and job table row say now, which is recorded
// in the history on the way. A job whose Probe only reads memory (InMemory) and that started before this
// process did is answered from the history alone: its recorded end, or a failure when none was recorded.
func (j *Jobs) statusOf(ctx context.Context, h jobhistory.Entry) (JobStatus, error) {
	k, known := j.kind(h.Kind)
	if !known && h.QueueJobID == "" { // nothing reads this kind any more (the tool package is gone): what was recorded is all there is
		return statusFrom(h), nil
	}
	if k.inMemory && h.StartedAt.Before(j.bootedAt) {
		if !h.Open() {
			return statusFrom(h), nil
		}
		st := statusFrom(h)
		st.State, st.Error, st.ErrorCode, st.ErrorID = JobFailed, lostMessage, "", ""
		j.record(ctx, h, &st, k)
		return st, nil
	}
	st, gone, err := j.live(ctx, &entry{JobInfo: JobInfo{ID: h.ID, Kind: h.Kind, Target: h.Target, StartedAt: h.StartedAt}, queueJobID: h.QueueJobID})
	if err != nil {
		return JobStatus{}, err
	}
	if gone { // the job table row is gone: the recorded end is all there is
		if h.Open() {
			return JobStatus{}, service.ErrNotFound
		}
		return statusFrom(h), nil
	}
	j.record(ctx, h, &st, k)
	return st, nil
}

// Observe reads the status of the job and records it: a tool that knows its work has ended calls it, so the
// history has the end even when nobody asks. An unknown job and a failed read are not errors.
func (j *Jobs) Observe(ctx context.Context, id string) {
	if h, err := j.history.Get(ctx, id); err == nil {
		_, _ = j.statusOf(ctx, h)
	}
}

// RefreshOpen looks at the jobs that are not over again, so that the history shows their end. A job that
// cannot be read (its Shot was deleted, ...) is skipped.
func (j *Jobs) RefreshOpen(ctx context.Context) error {
	open, err := j.history.Open(ctx, maxRefresh)
	if err != nil {
		return err
	}
	for _, h := range open {
		_, _ = j.statusOf(ctx, h)
	}
	return nil
}

// record completes st with what the history knows (the Project, the failure's code, when it ended) and
// writes the state back when it changed. A failed write is logged: the answer does not depend on it.
func (j *Jobs) record(ctx context.Context, h jobhistory.Entry, st *JobStatus, k kind) {
	st.ProjectID = h.ProjectID
	if st.State == JobFailed {
		st.ErrorCode = errorCodeOf(st.Error)
	}
	st.EndedAt = nil
	if st.State.Final() {
		st.EndedAt = endedAt(h, st, j.now().UTC().Truncate(time.Millisecond))
	}
	n := h
	n.State, n.Message, n.Done, n.Total = string(st.State), st.Message, st.Done, st.Total
	n.ErrorCode, n.ErrorMessage, n.ErrorID = st.ErrorCode, st.Error, st.ErrorID
	n.FinishedAt, n.EndedAt = st.FinishedAt, st.EndedAt
	n.Result = nil
	if k.inMemory && len(st.Result) > 0 { // nothing else can produce the result again
		if b, err := json.Marshal(st.Result); err == nil {
			n.Result = b
		}
	}
	if sameState(h, n) {
		return
	}
	if err := j.history.Settle(context.WithoutCancel(ctx), n); err != nil && j.logger != nil && !errors.Is(err, repository.ErrNotFound) {
		j.logger.Warn("record job history failed", "job_id", h.ID, "err", err)
	}
}

// endedAt is the end of a final job: the time the job queue recorded, else the first time it was seen over.
func endedAt(h jobhistory.Entry, st *JobStatus, now time.Time) *time.Time {
	switch {
	case st.FinishedAt != nil:
		return st.FinishedAt
	case !h.Open() && h.EndedAt != nil:
		return h.EndedAt
	}
	return &now
}

func sameState(a, b jobhistory.Entry) bool {
	return a.State == b.State && a.Message == b.Message && a.Done == b.Done && a.Total == b.Total &&
		a.ErrorCode == b.ErrorCode && a.ErrorMessage == b.ErrorMessage && a.ErrorID == b.ErrorID &&
		string(a.Result) == string(b.Result) && sameTime(a.FinishedAt, b.FinishedAt) && sameTime(a.EndedAt, b.EndedAt)
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// statusFrom is the status the history recorded.
func statusFrom(h jobhistory.Entry) JobStatus {
	st := JobStatus{
		JobID: h.ID, Kind: h.Kind, Target: h.Target, State: JobState(h.State), Message: h.Message, Done: h.Done, Total: h.Total,
		Error: h.ErrorMessage, ErrorCode: h.ErrorCode, ErrorID: h.ErrorID, ProjectID: h.ProjectID, StartedAt: h.StartedAt, FinishedAt: h.FinishedAt, EndedAt: h.EndedAt,
	}
	if len(h.Result) > 0 {
		_ = json.Unmarshal(h.Result, &st.Result) // a result that does not read back is left out
	}
	return st
}

// errorCodeOf is the <code> of an error written "<code>: <message>" (MapError's), "" for any other text (a
// reason the job queue wrote).
func errorCodeOf(s string) string {
	for _, c := range errorCodes {
		if strings.HasPrefix(s, c+": ") {
			return c
		}
	}
	return ""
}
