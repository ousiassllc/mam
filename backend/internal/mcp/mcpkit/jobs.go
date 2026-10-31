package mcpkit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobhistory"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// JobState is where a long operation stands. The persisted job table has no "cancelled"; a Probe reports it
// for work that can be stopped.
type JobState string

// The states of a job. Succeeded, Failed and Cancelled are final.
const (
	JobQueued    JobState = "queued"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

// Final reports whether the state never changes again.
func (s JobState) Final() bool { return s == JobSucceeded || s == JobFailed || s == JobCancelled }

// JobRef is what a tool that starts a long operation returns: embed it in the output struct.
type JobRef struct {
	JobID string   `json:"job_id" jsonschema:"get_job で進捗と結果を取る ID / poll it with get_job"`
	State JobState `json:"state" jsonschema:"受け付けた時点の状態（queued）/ state when accepted (queued)"`
}

// JobStatus is what get_job answers: the state, the progress and, when the work is over, the result or the reason.
type JobStatus struct {
	JobID      string         `json:"job_id"`
	Kind       string         `json:"kind" jsonschema:"操作の種類 / what was started (e.g. generate_shot)"`
	Target     string         `json:"target,omitempty" jsonschema:"操作の対象の ID（shot_id など）/ id of the object it works on"`
	State      JobState       `json:"state"`
	Message    string         `json:"message,omitempty" jsonschema:"いま何をしているか / what it is doing now"`
	Done       int            `json:"done,omitempty" jsonschema:"終わった数（Take など）/ items finished"`
	Total      int            `json:"total,omitempty" jsonschema:"全体の数 / items in all"`
	Result     map[string]any `json:"result,omitempty" jsonschema:"成功したときの結果（take_id など）/ result when it succeeded (e.g. take_ids)"`
	Error      string         `json:"error,omitempty" jsonschema:"失敗の理由 / why it failed"`
	ErrorID    string         `json:"error_id,omitempty" jsonschema:"internal の失敗の相関 ID。get_app_logs の attrs に {\"error_id\": …} で渡すと原因のログ行が見つかる / correlation id of an internal failure: find its cause with get_app_logs (attrs error_id)"`
	ErrorCode  string         `json:"error_code,omitempty" jsonschema:"失敗のコード（error の <code>: <message> の <code>。validation・conflict・budget_exceeded・upstream・internal など）/ code of the failure"`
	ProjectID  string         `json:"project_id,omitempty" jsonschema:"対象が属する Project の ID（無ければ省略）/ Project of the target, when it has one"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt *time.Time     `json:"finished_at,omitempty" jsonschema:"ジョブキューが終了を記録した時刻 / when the job queue recorded the end"`
	EndedAt    *time.Time     `json:"ended_at,omitempty" jsonschema:"終わった時刻（ジョブキューの記録があればそれ、無ければ mam が終了を最初に確かめた時刻）/ when it ended: the job queue's time, else when mam first saw the end"`
}

// JobInfo is what a Probe is told about the job it looks at.
type JobInfo struct {
	ID        string
	Kind      string
	Target    string
	StartedAt time.Time
}

// Progress is what a Probe reads from the persisted state of the work. Only State is required.
type Progress struct {
	State   JobState
	Message string
	Done    int
	Total   int
	Result  map[string]any
	Error   string
	// ErrorID is the correlation id of an internal failure (ReportError); "" for any other state.
	ErrorID string
}

// Probe derives the Progress of one kind of job from the state the service layer persisted (a Shot's
// state, the Takes it has, ...), so that get_job can be asked again and again, by any client, and still
// answers after the work ended. It must not change anything.
type Probe func(ctx context.Context, job JobInfo) (Progress, error)

// KindOption adjusts a job kind registered with RegisterKind.
type KindOption func(*kind)

// InMemory marks a kind whose Probe reads state that only lives in memory (a tracker of the running
// stage, the status of an install): a restart forgets it, so a job of this kind that started before this
// process did is answered from the history only (see Jobs).
func InMemory() KindOption { return func(k *kind) { k.inMemory = true } }

type kind struct {
	probe    Probe
	inMemory bool
}

// JobTable is the persisted job queue (repository.JobRepository).
type JobTable interface {
	Get(ctx context.Context, id string) (domain.Job, error)
}

const jobIDStart = "job_"

type entry struct {
	JobInfo
	queueJobID string // id of the row in the job table, "" when the work is not a queue job
}

// Jobs turns the start of a long operation into a job_id and the job_id back into a JobStatus.
//
// A job is the pair (kind, target) plus, when the service can say it, the id of the row in the persisted
// job table. Its state comes from two places: the job table says queued, running, failed (with the
// reason) and, once finished, hands over to the Probe of the kind, which reads what the work produced.
// The identity of every job (id, kind, target, start, queue job id) is kept in the history table
// (JobHistory), so a job_id works after a restart; the id of a job table row is also accepted by get_job
// as it is. Whatever Get finds (state, message, failure, end) is written back to the history, so that
// list_jobs can search it and so that the end of a job is still known when nothing can read it any more.
type Jobs struct {
	table    JobTable
	history  JobHistory
	now      func() time.Time
	bootedAt time.Time    // when this process made the Jobs: an InMemory job older than this lost its state
	logger   *slog.Logger // set by NewRegistrar; nil discards

	mu    sync.Mutex
	kinds map[string]kind
}

// NewJobs returns Jobs over the job table and the history. table may be nil (tests, and builds without a
// job queue).
func NewJobs(table JobTable, history JobHistory) *Jobs {
	now := time.Now
	return &Jobs{table: table, history: history, now: now, bootedAt: now().UTC().Truncate(time.Millisecond), kinds: map[string]kind{}}
}

// WithClock makes the Jobs read the time from now (tests of the history move the clock). The start of the
// process, which separates the jobs of an InMemory kind that survive from the ones a restart ended, is the
// time of this call.
func (j *Jobs) WithClock(now func() time.Time) *Jobs {
	j.now, j.bootedAt = now, now().UTC().Truncate(time.Millisecond)
	return j
}

// RegisterKind installs the Probe of a kind of job. A tool package calls it once, from its Register.
func (j *Jobs) RegisterKind(name string, probe Probe, opts ...KindOption) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, dup := j.kinds[name]; dup {
		panic(fmt.Sprintf("mcpkit: job kind %q is registered twice", name))
	}
	k := kind{probe: probe}
	for _, o := range opts {
		o(&k)
	}
	j.kinds[name] = k
}

func (j *Jobs) kind(name string) (kind, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	k, ok := j.kinds[name]
	return k, ok
}

// Run starts a long operation and returns its JobRef. start calls the service (e.g.
// svc.Generation.GenerateShot) and returns the id of the job table row it queued, or "" when the service
// does not say. Run notes the time before start runs, so that a Probe can tell what the work produced from
// what was there before. A kind with no Probe needs a queue job id: there would be nothing to read
// the progress from.
func (j *Jobs) Run(ctx context.Context, kind, target string, start func(ctx context.Context) (queueJobID string, err error)) (JobRef, error) {
	return j.RunResolved(ctx, kind, func(ctx context.Context) (string, string, error) {
		queueID, err := start(ctx)
		return target, queueID, err
	})
}

// RunResolved is Run for work whose target is only known once it has started (Continue makes the Shot it
// generates): start returns the target together with the queue job id.
func (j *Jobs) RunResolved(ctx context.Context, name string, start func(ctx context.Context) (target, queueJobID string, err error)) (JobRef, error) {
	_, probed := j.kind(name)
	startedAt := j.now().UTC().Truncate(time.Millisecond) // the precision the history keeps
	target, queueID, err := start(ctx)
	if err != nil {
		return JobRef{}, err
	}
	if !probed && queueID == "" {
		return JobRef{}, fmt.Errorf("mcpkit: job kind %q has no Probe and start returned no queue job id", name)
	}
	id := queueID
	if id == "" {
		id = jobIDStart + domain.NewID()
	}
	// The work has started: the row is written even when the caller has gone.
	h := jobhistory.Entry{ID: id, Kind: name, Target: target, QueueJobID: queueID, StartedAt: startedAt, State: string(JobQueued)}
	if err := j.history.Insert(context.WithoutCancel(ctx), h); err != nil {
		return JobRef{}, fmt.Errorf("mcpkit: record job %s: %w", id, err)
	}
	return JobRef{JobID: id, State: JobQueued}, nil
}

// Get returns the status of a job: a job_id Run returned (also after a restart), or the id of a job table
// row. It is service.ErrNotFound for any other id.
func (j *Jobs) Get(ctx context.Context, id string) (JobStatus, error) {
	h, err := j.history.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		row, err := j.row(ctx, id)
		if err != nil {
			return JobStatus{}, err
		}
		st, _, err := j.live(ctx, &entry{JobInfo: JobInfo{ID: id, Kind: string(row.Kind), StartedAt: row.CreatedAt}, queueJobID: id})
		return st, err
	}
	if err != nil {
		return JobStatus{}, err
	}
	return j.statusOf(ctx, h)
}

func (j *Jobs) row(ctx context.Context, id string) (domain.Job, error) {
	if j.table == nil {
		return domain.Job{}, service.ErrNotFound
	}
	row, err := j.table.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Job{}, service.ErrNotFound
	}
	return row, err
}

// live reads the state of the job from the Probe of its kind and the job table row. gone reports that the
// row the job names no longer exists; the status is then what the Probe said, and the caller may know better.
func (j *Jobs) live(ctx context.Context, e *entry) (_ JobStatus, gone bool, _ error) {
	st := JobStatus{JobID: e.ID, Kind: e.Kind, Target: e.Target, State: JobRunning, StartedAt: e.StartedAt}

	k, _ := j.kind(e.Kind)
	if k.probe != nil {
		p, err := k.probe(ctx, e.JobInfo)
		if err != nil {
			return JobStatus{}, false, err
		}
		st.State, st.Message, st.Done, st.Total, st.Result, st.Error, st.ErrorID = p.State, p.Message, p.Done, p.Total, p.Result, p.Error, p.ErrorID
	}
	if e.queueJobID != "" {
		row, err := j.row(ctx, e.queueJobID)
		if errors.Is(err, service.ErrNotFound) {
			return st, true, nil
		}
		if err != nil {
			return JobStatus{}, false, err
		}
		applyRow(&st, row, k.probe != nil)
	}
	return st, false, nil
}

// applyRow lets the job table override the probe where it knows better: a queued, running or failed
// row decides (a failed row has the reason), a succeeded row decides only when no Probe reads the work.
func applyRow(st *JobStatus, row domain.Job, probed bool) {
	switch row.Status {
	case domain.JobQueued:
		st.State = JobQueued
	case domain.JobRunning:
		st.State = JobRunning
	case domain.JobFailed:
		st.State, st.Error, st.ErrorID = JobFailed, row.Error, ""
	case domain.JobSucceeded:
		if !probed {
			st.State = JobSucceeded
		}
	}
	if row.FinishedAt != nil {
		st.FinishedAt = row.FinishedAt
	}
}
