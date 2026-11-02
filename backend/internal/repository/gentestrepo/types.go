// Package gentestrepo is the repository of the generation test screen's history (tables gentest_batch, gentest_input,
// gentest_result, gentest_prompt and gentest_import; spec/architecture/gentest/er-gentest.md). It sits beside the
// per-aggregate repositories of the parent package and uses its DB and its error values. None of the tables references a
// Project table, so nothing here is project data: no take, no cost ledger row, no asset.
package gentestrepo

import (
	"encoding/json"
	"time"
)

// Media of a batch and of a result.
const (
	MediaImage = "image"
	MediaVideo = "video"
)

// Roles of an input image.
const (
	RoleReference  = "reference"
	RoleFirstFrame = "first_frame"
	RoleLastFrame  = "last_frame"
	// The roles of a reference of an image model besides the whole picture (RoleReference).
	RoleFace  = "face"
	RoleStyle = "style"
	RoleEdit  = "edit"
)

// Status of a result. queued -> running -> succeeded | failed; a queued result can also be cancelled.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Negative modes: how the negative prompt of a result reached the model.
const (
	NegativeNone   = "none"   // there was none
	NegativeField  = "field"  // sent in the model's negative prompt field
	NegativeFolded = "folded" // written into the prompt as an "Avoid: ..." sentence
)

// Cost bases: how the estimated cost of a result was known.
const (
	CostPriced  = "priced"  // EstimatedCost is the price in USD
	CostFree    = "free"    // a local model: 0 USD
	CostUnknown = "unknown" // no price (a subscription, a cloud model without a price): EstimatedCost is nil
)

// Batch is one run of the screen: the media, the parameters, the estimate that was confirmed, and the job.
type Batch struct {
	ID    string
	Media string
	JobID string
	// Params is {"aspect_ratio", "resolution", "duration_seconds", "count"} (the service owns the shape).
	Params         json.RawMessage
	EstimatedCost  float64
	Estimate       json.RawMessage
	CreatedAt      time.Time
	FinishedAt     *time.Time
	InputCount     int // set by GetBatch / ListBatchesOf: not stored
	ResultCount    int
	SucceededCount int
}

// Input is an image the batch was given. FilePath is relative to the gentest directory.
type Input struct {
	ID        string
	BatchID   string
	Role      string
	FilePath  string
	MimeType  string
	SizeBytes int64
	// Origin is {"kind": "upload"|"project_asset"|"reference_item"|"history", "id": ..., "project_id": ...}.
	Origin   json.RawMessage
	Position int
	// Strength (0..1) is how strongly an image model shows the reference; nil is the default (and not applicable to a frame).
	Strength *float64
}

// Result is one generation. FilePath is relative to the gentest directory; empty until it succeeded.
type Result struct {
	ID             string
	BatchID        string
	Position       int
	Media          string
	Model          string
	APIModel       string
	Prompt         string
	NegativePrompt string
	SentPrompt     string
	NegativeMode   string
	// Params is {"aspect_ratio", "resolution", "duration_seconds"}: what was asked of this model (the length snapped).
	Params json.RawMessage
	// UsedInputs are the ids of the batch's inputs this model was given.
	UsedInputs []string
	// Seed is nil for a model that takes none; SeedSent says it went to the vendor.
	Seed          *int64
	SeedSent      bool
	Status        string
	Error         string
	FilePath      string
	MimeType      string
	SizeBytes     int64
	CostBasis     string
	EstimatedCost *float64
	ActualCost    *float64
	Favorite      bool
	CreatedAt     time.Time
	FinishedAt    *time.Time
}

// Prompt is a prompt that was used, once per (prompt, negative prompt).
type Prompt struct {
	ID             string
	Prompt         string
	NegativePrompt string
	UseCount       int
	CreatedAt      time.Time
	LastUsedAt     time.Time
}

// PromptKey is a prompt a batch used.
type PromptKey struct{ Prompt, NegativePrompt string }

// Import records where a result was imported to. The ids are plain TEXT: the Project may be deleted later.
type Import struct {
	ID              string
	ResultID        string
	ProjectID       string
	AssetID         string
	ReferenceItemID string
	// Target is {"as": "reference"|"asset", "kind", "name", "tags", and "assign": {"target","id","slot"} when it was assigned}.
	Target    json.RawMessage
	CreatedAt time.Time
}

// NewBatch is everything one run writes at once: the batch, its inputs, its results (all queued) and the prompts it used.
type NewBatch struct {
	Batch   Batch
	Inputs  []Input
	Results []Result
	Prompts []PromptKey
}

// Settlement is how a result ended.
type Settlement struct {
	Status     string // succeeded | failed | cancelled
	Error      string
	FilePath   string
	MimeType   string
	SizeBytes  int64
	ActualCost *float64
	At         time.Time
}

// Filter narrows ListResults. Every set field must match.
type Filter struct {
	Model string
	// Models keeps the results of any of these models (with Model: both must match).
	Models       []string
	Media        string
	FavoriteOnly bool
	BatchID      string
	Status       string
	// SucceededOnly keeps the results that have a file (the "pick from history" lists).
	SucceededOnly bool
	Limit         int // 0: no limit
	Offset        int
}

// Counts are the results of a batch by status.
type Counts struct {
	Queued, Running, Succeeded, Failed, Cancelled int
}

// Total is the number of results.
func (c Counts) Total() int { return c.Queued + c.Running + c.Succeeded + c.Failed + c.Cancelled }

// Done is the number of results that are over.
func (c Counts) Done() int { return c.Succeeded + c.Failed + c.Cancelled }

// Spend is the one-line total of the history: the results that made a file, by how their cost was known.
type Spend struct {
	Items        int
	PricedUSD    float64 // the actual cost where the vendor reported one, else the estimate
	UnknownItems int     // results without a price: not in PricedUSD
}
