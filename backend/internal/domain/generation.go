package domain

import (
	"encoding/json"
	"time"

	mr "github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

// MaxInstructionRunes bounds the additional instruction of a generation.
const MaxInstructionRunes = 1000

// ModelName identifies an image or video generation model. The values are the canonical
// identifiers of spec/architecture/model-adapters.md §6, stored in take.model_name and
// cost_ledger_entry.provider.
type ModelName string

// The built-in image models (the first four), then the video models.
const (
	ModelNanoBanana      ModelName = "nano-banana"
	ModelNanoBananaLite  ModelName = "nano-banana-lite" // Gemini 3.1 Flash Lite Image: the cheap, fast 1K model
	ModelFlux2           ModelName = "flux-2"
	ModelGPTImage2       ModelName = "gpt-image-2"
	ModelKling3          ModelName = "kling-3.0"
	ModelSeedance2       ModelName = "seedance-2.0"
	ModelRunwayGen45     ModelName = "runway-gen-4.5"
	ModelGeminiOmniFlash ModelName = "gemini-omni-flash" // Gemini Omni Flash (gemini-omni-1.1-flash), the Gemini API's video model
	ModelKling4          ModelName = "kling-4.0"         // Kling 4.0 on the new API (Bearer API key); ModelKling3 stays on the old one
	ModelSeedance25      ModelName = "seedance-2.5"      // Seedance 2.5 (dreamina-seedance-2-5-260628), up to 30 seconds
)

// ImageModels lists the image models (built-in, then defined on registered endpoints: modelreg).
func ImageModels() []ModelName {
	return mr.Image(ModelNanoBanana, ModelNanoBananaLite, ModelFlux2, ModelGPTImage2)
}

// VideoModels lists the video models that have an adapter (built-in, then defined on registered endpoints).
func VideoModels() []ModelName {
	return mr.Video(ModelKling3, ModelSeedance2, ModelRunwayGen45, ModelGeminiOmniFlash, ModelKling4, ModelSeedance25)
}

// JoinModels renders models separated by sep ("kling-3.0 / seedance-2.0 / runway-gen-4.5" for " / ").
func JoinModels(models []ModelName, sep string) string { return mr.Join(models, sep) }

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
