package mcpkit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
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
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt *time.Time     `json:"finished_at,omitempty" jsonschema:"ジョブキューが終了を記録した時刻 / when the job queue recorded the end"`
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
}

// Probe derives the Progress of one kind of job from the state the service layer persisted (a Shot's
// state, the Takes it has, ...), so that get_job can be asked again and again, by any client, and still
// answers after the work ended. It must not change anything.
type Probe func(ctx context.Context, job JobInfo) (Progress, error)

// JobTable is the persisted job queue (repository.JobRepository).
type JobTable interface {
	Get(ctx context.Context, id string) (domain.Job, error)
}

// Registry limits: a job nobody asks about for a day is forgotten, and so is the oldest when there are too many.
const (
	jobTTL     = 24 * time.Hour
	maxJobs    = 4096
	jobIDStart = "job_"
)

type entry struct {
	JobInfo
	queueJobID string // id of the row in the job table, "" when the work is not a queue job
}

// Jobs turns the start of a long operation into a job_id and the job_id back into a JobStatus.
//
// A job is the pair (kind, target) plus, when the service can say it, the id of the row in the persisted
// job table. Its state comes from two places: the job table says queued, running, failed (with the
// reason) and, once finished, hands over to the Probe of the kind, which reads what the work produced.
// The ids live in memory (a restart forgets them); the id of a job table row is accepted by get_job
// as it is, and survives restarts.
type Jobs struct {
	table JobTable
	now   func() time.Time

	mu      sync.Mutex
	probes  map[string]Probe
	entries map[string]*entry
	order   []string // ids, oldest first
}

// NewJobs returns Jobs over the job table. table may be nil (tests, and builds without a job queue).
func NewJobs(table JobTable) *Jobs {
	return &Jobs{table: table, now: time.Now, probes: map[string]Probe{}, entries: map[string]*entry{}}
}

// RegisterKind installs the Probe of a kind of job. A tool package calls it once, from its Register.
func (j *Jobs) RegisterKind(kind string, probe Probe) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, dup := j.probes[kind]; dup {
		panic(fmt.Sprintf("mcpkit: job kind %q is registered twice", kind))
	}
	j.probes[kind] = probe
}

// Run starts a long operation and returns its JobRef. start calls the service (e.g.
// svc.Generation.GenerateShot) and returns the id of the job table row it queued, or "" when the service
// does not say. Run notes the time before start runs, so that a Probe can tell what the work produced from
// what was there before. A kind with no Probe needs a queue job id: there would be nothing to read
// the progress from.
func (j *Jobs) Run(ctx context.Context, kind, target string, start func(ctx context.Context) (queueJobID string, err error)) (JobRef, error) {
	j.mu.Lock()
	_, probed := j.probes[kind]
	j.mu.Unlock()
	startedAt := j.now().UTC()
	queueID, err := start(ctx)
	if err != nil {
		return JobRef{}, err
	}
	if !probed && queueID == "" {
		return JobRef{}, fmt.Errorf("mcpkit: job kind %q has no Probe and start returned no queue job id", kind)
	}
	id := queueID
	if id == "" {
		id = jobIDStart + domain.NewID()
	}
	e := &entry{JobInfo: JobInfo{ID: id, Kind: kind, Target: target, StartedAt: startedAt}, queueJobID: queueID}
	j.remember(e)
	return JobRef{JobID: id, State: JobQueued}, nil
}

func (j *Jobs) remember(e *entry) {
	j.mu.Lock()
	defer j.mu.Unlock()
	cutoff := j.now().Add(-jobTTL)
	for len(j.order) > 0 {
		oldest := j.entries[j.order[0]]
		if oldest != nil && len(j.order) < maxJobs && !oldest.StartedAt.Before(cutoff) {
			break
		}
		delete(j.entries, j.order[0])
		j.order = j.order[1:]
	}
	if _, known := j.entries[e.ID]; !known {
		j.order = append(j.order, e.ID)
	}
	j.entries[e.ID] = e
}

// Get returns the status of a job: a job_id Run returned, or the id of a job table row. It is
// service.ErrNotFound for any other id.
func (j *Jobs) Get(ctx context.Context, id string) (JobStatus, error) {
	j.mu.Lock()
	e := j.entries[id]
	j.mu.Unlock()
	if e == nil {
		row, err := j.row(ctx, id)
		if err != nil {
			return JobStatus{}, err
		}
		e = &entry{JobInfo: JobInfo{ID: id, Kind: string(row.Kind), StartedAt: row.CreatedAt}, queueJobID: id}
	}
	return j.status(ctx, e)
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

func (j *Jobs) status(ctx context.Context, e *entry) (JobStatus, error) {
	st := JobStatus{JobID: e.ID, Kind: e.Kind, Target: e.Target, State: JobRunning, StartedAt: e.StartedAt}

	j.mu.Lock()
	probe := j.probes[e.Kind]
	j.mu.Unlock()
	if probe != nil {
		p, err := probe(ctx, e.JobInfo)
		if err != nil {
			return JobStatus{}, err
		}
		st.State, st.Message, st.Done, st.Total, st.Result, st.Error = p.State, p.Message, p.Done, p.Total, p.Result, p.Error
	}
	if e.queueJobID != "" {
		row, err := j.row(ctx, e.queueJobID)
		if err != nil {
			return JobStatus{}, err
		}
		applyRow(&st, row, probe != nil)
	}
	return st, nil
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
		st.State, st.Error = JobFailed, row.Error
	case domain.JobSucceeded:
		if !probed {
			st.State = JobSucceeded
		}
	}
	if row.FinishedAt != nil {
		st.FinishedAt = row.FinishedAt
	}
}
