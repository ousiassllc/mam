package domain

import (
	"encoding/json"
	"time"
)

// JobKind identifies the handler that executes a background job.
type JobKind string

// Background job kinds (spec/requirements/non-functional.md §2).
const (
	JobGenerate     JobKind = "generate"
	JobJudge        JobKind = "judge"
	JobRepair       JobKind = "repair"
	JobPlan         JobKind = "plan"          // the planning stages of the AUTO pipeline (Producer to Storyboard) for one Project
	JobModelInstall JobKind = "model_install" // model manager: download of the engine or a model (resumable)
	JobGentest      JobKind = "gentest"       // model manager: a trial generation outside any Project (not resumable)
)

// JobKinds lists every valid JobKind.
func JobKinds() []JobKind { return append([]JobKind(nil), jobKinds...) }

var jobKinds = []JobKind{JobGenerate, JobJudge, JobRepair, JobPlan, JobModelInstall, JobGentest}

// JobStatus is the lifecycle state of a background job.
type JobStatus string

// Job statuses. queued -> running -> succeeded | failed. Terminal states never change.
const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
)

// JobStatuses lists every valid JobStatus.
func JobStatuses() []JobStatus { return []JobStatus{JobQueued, JobRunning, JobSucceeded, JobFailed} }

// Job is a persisted background job: unfinished work is found after an abnormal termination and resumed or failed.
type Job struct {
	ID         string
	Kind       JobKind
	Payload    json.RawMessage
	Status     JobStatus
	Attempts   int
	Error      string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}
