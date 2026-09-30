package domain

import "encoding/json"

// TakeLineage is the part of take.model_parameters that records what the Take was generated from
// besides its Shot Spec (F-27, spec/spec.md §29.3): the World State version the spec was planned
// against and the References the model received. Generation writes it beside the model parameters.
type TakeLineage struct {
	// WorldStateVersion is the number of the latest world_state_version when the Take was made; 0
	// when no Shot had completed yet (or the Take predates the record).
	WorldStateVersion int         `json:"world_state_version"`
	References        []Reference `json:"references,omitempty"`
}

// TraceOutput is the file a Take produced.
type TraceOutput struct {
	AssetID   string
	FilePath  string
	MimeType  string
	SizeBytes int64
}

// TakeTrace is everything that led to a Take (F-27): the Source Shot Spec and Prompt version, the
// World State version, the Model and its parameters, the References, the Output, the Judge
// results and the Repair history.
type TakeTrace struct {
	ProjectID string
	Take      Take
	// ModelParameters is take.model_parameters as stored.
	ModelParameters json.RawMessage
	// Routing is why the video model was chosen (Multi-model routing, M3); nil for a Take that
	// predates the record.
	Routing *RoutingDecision
	// PromptVersion holds the Source Shot Spec (SpecSnapshot) and what the Model Adapter derived from it.
	PromptVersion PromptVersion
	// WorldStateVersion is 0 when unknown or when no World State version existed yet.
	WorldStateVersion int
	// References are those the model received; for a Take that predates the record, the Reference Pack of its Shot Spec.
	References []Reference
	// Output is nil when the Take's file was archived.
	Output *TraceOutput
	// Judgements are the Judge results, newest first.
	Judgements []JudgeResult
	// Repairs are the repairs of this Take by the Repair Agent, oldest first.
	Repairs []RepairRecord
	// ProducedBy is the repair that made this Take; nil for a first generation.
	ProducedBy *RepairRecord
}
