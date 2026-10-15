package domain

import (
	"encoding/json"
	"strings"
	"time"
)

// MaxInstructionRunes bounds the additional instruction of a generation.
const MaxInstructionRunes = 1000

// ModelName identifies an image or video generation model. The values are the canonical
// identifiers of spec/architecture/model-adapters.md §6, stored in take.model_name and
// cost_ledger_entry.provider.
type ModelName string

// Image models.
const (
	ModelImagen4    ModelName = "imagen-4"
	ModelNanoBanana ModelName = "nano-banana"
	ModelFlux2      ModelName = "flux-2"
	ModelGPTImage2  ModelName = "gpt-image-2"
)

// Video models.
const (
	ModelKling3      ModelName = "kling-3.0"
	ModelSeedance2   ModelName = "seedance-2.0"
	ModelRunwayGen45 ModelName = "runway-gen-4.5"
)

// ImageModels lists the image models.
func ImageModels() []ModelName {
	return []ModelName{ModelImagen4, ModelNanoBanana, ModelFlux2, ModelGPTImage2}
}

// VideoModels lists the video models that have an adapter.
func VideoModels() []ModelName { return []ModelName{ModelKling3, ModelSeedance2, ModelRunwayGen45} }

// JoinModels renders models separated by sep ("kling-3.0 / seedance-2.0 / runway-gen-4.5" for " / ").
func JoinModels(models []ModelName, sep string) string {
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = string(m)
	}
	return strings.Join(names, sep)
}

// PromptVersion is a row of prompt_version: the Shot Spec a generation started from and what the
// Model Adapter derived from it.
type PromptVersion struct {
	ID            string
	ShotID        string
	VersionNumber int
	SpecSnapshot  json.RawMessage
	AdapterOutput json.RawMessage
	// NoteRefs are the note versions the Prompt Planner was given (prompt_version.note_refs, F-31).
	NoteRefs  []NoteRef
	CreatedAt time.Time
}

// CostEntry is a row of cost_ledger_entry. Provider is a ModelName. ShotID and TakeID may be empty.
// ActualCost is the cost the vendor reported for the call (nil when it reports none).
type CostEntry struct {
	ProjectID     string
	ShotID        string
	TakeID        string
	Provider      ModelName
	EstimatedCost float64
	ActualCost    *float64
}
