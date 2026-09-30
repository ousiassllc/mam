package domain

import "time"

// ReshootStatus is where an additional Shot asked for by the Editor Judge is in the Editor →
// Reshoot Loop (spec/spec.md §17.2, F-19).
type ReshootStatus string

// Reshoot statuses.
const (
	// ReshootRequested: the Director has been asked; the Shot is being made.
	ReshootRequested ReshootStatus = "requested"
	// ReshootApplied: the Shot has a Selected Take and was put into the Edit Timeline.
	ReshootApplied ReshootStatus = "applied"
)

// ReshootRequest is one additional Shot the Editor Judge asked the Director for
// (reshoot_request). Every call of request-additional-shot makes one round of requests; the Retry
// Policy counts rounds.
type ReshootRequest struct {
	ID        string
	ProjectID string
	// BranchID is the Branch whose timeline asked; "" for the trunk.
	BranchID string
	// EditTimelineVersionID is the version the Editor Judge judged.
	EditTimelineVersionID string
	Round                 int
	// ShotID is the Shot the Director made for the request.
	ShotID string
	// AfterShotID is the Shot of the clip the new clip follows ("" puts it at the end).
	AfterShotID     string
	Kind            ClipKind
	Objective       string
	DurationSeconds float64
	Status          ReshootStatus
	// AppliedEditTimelineVersionID is the version that has the new clip (once applied).
	AppliedEditTimelineVersionID string
	CreatedAt                    time.Time
}
