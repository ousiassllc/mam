package domain

import (
	"encoding/json"
	"time"
)

// JobKind identifies the handler that executes a background job.
type JobKind string

// Background job kinds (spec/requirements/non-functional.md §2).
const (
	JobGenerate JobKind = "generate"
	JobJudge    JobKind = "judge"
	JobRepair   JobKind = "repair"
	// JobPlan runs the planning stages of the AUTO pipeline (Producer to Storyboard) for one Project.
	JobPlan JobKind = "plan"
)

// JobKinds lists every valid JobKind.
func JobKinds() []JobKind { return []JobKind{JobGenerate, JobJudge, JobRepair, JobPlan} }

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
func JobStatuses() []JobStatus {
	return []JobStatus{JobQueued, JobRunning, JobSucceeded, JobFailed}
}

// Job is a persisted background job. Persisting jobs lets the app detect
// unfinished work after an abnormal termination and resume it or fail it safely.
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
