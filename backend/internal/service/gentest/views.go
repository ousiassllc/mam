package gentest

import (
	"encoding/json"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"

	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// The media of a result (ResultView.Media), the status of a result (ResultView.Status) and the role of an input image.
const (
	MediaImage      = gentestrepo.MediaImage
	MediaVideo      = gentestrepo.MediaVideo
	StatusQueued    = gentestrepo.StatusQueued
	StatusRunning   = gentestrepo.StatusRunning
	StatusSucceeded = gentestrepo.StatusSucceeded
	StatusFailed    = gentestrepo.StatusFailed
	StatusCancelled = gentestrepo.StatusCancelled
	RoleReference   = gentestrepo.RoleReference
	RoleFirstFrame  = gentestrepo.RoleFirstFrame
	RoleLastFrame   = gentestrepo.RoleLastFrame
	RoleFace        = gentestrepo.RoleFace
	RoleStyle       = gentestrepo.RoleStyle
	RoleEdit        = gentestrepo.RoleEdit
)

// ResultView is one result as the screen and the MCP tools show it.
type ResultView struct {
	ID         string `json:"result_id"`
	BatchID    string `json:"batch_id"`
	Position   int    `json:"position" jsonschema:"バッチの中で計画した順（モデル → プロンプト → 本数）"`
	Media      string `json:"media"`
	Model      string `json:"model" jsonschema:"正規のモデル識別子。代替モデルへは切り替えないので、実際に使ったモデルと同じ"`
	ModelLabel string `json:"model_label"`
	APIModel   string `json:"api_model"`
	Prompt     string `json:"prompt" jsonschema:"入力したプロンプト"`
	// SentPrompt is what was sent: the prompt, with the negative prompt written in when the model has no field for it.
	SentPrompt string `json:"sent_prompt"`
	// Translated says the prompt (and negative prompt) was translated into English before it was sent: TranslatedPrompt and
	// TranslatedNegativePrompt are that English. Absent when it was sent as typed (translation off, or English already).
	Translated               bool   `json:"translated"`
	TranslatedPrompt         string `json:"translated_prompt,omitempty"`
	TranslatedNegativePrompt string `json:"translated_negative_prompt,omitempty"`
	NegativePrompt           string `json:"negative_prompt,omitempty"`
	NegativeMode             string `json:"negative_mode" jsonschema:"none / field（モデルの欄に送った）/ folded（プロンプトに「Avoid: …」として書き足した）"`
	AspectRatio              string `json:"aspect_ratio"`
	Resolution               string `json:"resolution,omitempty"`
	// DurationSeconds is the length the model was asked for (rounded to what it makes).
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	// Seed is the seed that was sent; absent for a model that takes none (SeedSent false).
	Seed     *int64 `json:"seed,omitempty"`
	SeedSent bool   `json:"seed_sent"`
	Status   string `json:"status" jsonschema:"queued / running / succeeded / failed / cancelled"`
	Error    string `json:"error,omitempty"`
	HasFile  bool   `json:"has_file"`
	MimeType string `json:"mime_type,omitempty"`
	// SizeBytes is the size of the file.
	SizeBytes int64  `json:"size_bytes,omitempty"`
	CostBasis string `json:"cost_basis" jsonschema:"priced / free / unknown"`
	// EstimatedUSD is the estimate of this result (absent when the price is unknown); ActualUSD what the vendor reported, if it did.
	EstimatedUSD *float64 `json:"estimated_cost_usd,omitempty"`
	ActualUSD    *float64 `json:"actual_cost_usd,omitempty"`
	Favorite     bool     `json:"favorite"`
	UsedInputs   []string `json:"used_inputs,omitempty" jsonschema:"このモデルに渡した入力画像（get_generation_result の inputs の id）"`
	// AvoidReferencePose says the negative prompt got "do not copy the reference's pose and composition"; LoRA is the LoRA the model was given.
	AvoidReferencePose bool       `json:"avoid_reference_pose,omitempty"`
	LoRA               *refs.LoRA `json:"lora,omitempty" jsonschema:"このモデルに渡した LoRA（名前と強さ）"`
	CreatedAt          time.Time  `json:"created_at"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
}

func viewOf(r gentestrepo.Result) ResultView {
	var p resultParams
	_ = json.Unmarshal(r.Params, &p)
	v := ResultView{
		ID: r.ID, BatchID: r.BatchID, Position: r.Position, Media: r.Media, Model: r.Model, ModelLabel: modelreg.Label(r.Model), APIModel: r.APIModel,
		Prompt: r.Prompt, SentPrompt: r.SentPrompt, NegativePrompt: r.NegativePrompt, NegativeMode: r.NegativeMode,
		AspectRatio: p.AspectRatio, Resolution: p.Resolution, DurationSeconds: p.DurationSeconds, Seed: r.Seed, SeedSent: r.SeedSent,
		Status: r.Status, Error: r.Error, HasFile: r.Status == gentestrepo.StatusSucceeded && r.FilePath != "", MimeType: r.MimeType, SizeBytes: r.SizeBytes,
		CostBasis: r.CostBasis, EstimatedUSD: r.EstimatedCost, ActualUSD: r.ActualCost, Favorite: r.Favorite, UsedInputs: r.UsedInputs,
		AvoidReferencePose: p.AvoidPose, LoRA: p.LoRA,
		CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
	}
	if p.Translation != nil {
		v.Translated, v.TranslatedPrompt, v.TranslatedNegativePrompt = true, p.Translation.Prompt, p.Translation.Negative
	}
	return v
}

func viewsOf(list []gentestrepo.Result) []ResultView {
	out := make([]ResultView, len(list))
	for i, r := range list {
		out[i] = viewOf(r)
	}
	return out
}

// InputView is an input image of a batch.
type InputView struct {
	ID        string `json:"input_id"`
	Role      string `json:"role" jsonschema:"reference / face / style / edit / first_frame / last_frame"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	// Strength is how strongly an image model was asked to show the reference; absent when the default was used.
	Strength *float64    `json:"strength,omitempty"`
	Origin   InputOrigin `json:"origin" jsonschema:"どこから取った画像か"`
}

// InputOrigin is where an input image was taken from (the Source of the RefInput and its ID).
type InputOrigin struct {
	Kind      string `json:"kind" jsonschema:"upload / project_asset / reference_item / history"`
	ID        string `json:"id"`
	ProjectID string `json:"project_id,omitempty"`
}

// ImportView is a record of a result that was imported into a Project.
type ImportView struct {
	ID              string                   `json:"import_id"`
	ProjectID       string                   `json:"project_id"`
	AssetID         string                   `json:"asset_id"`
	ReferenceItemID string                   `json:"reference_item_id,omitempty"`
	Target          projectlink.ImportTarget `json:"target" jsonschema:"どう取り込んだか"`
	CreatedAt       time.Time                `json:"created_at"`
}

// ResultDetail is a result with its batch's inputs and where it was imported to.
type ResultDetail struct {
	ResultView
	Inputs  []InputView  `json:"inputs"`
	Imports []ImportView `json:"imports"`
	// Estimate is the confirmed estimate of the batch the result belongs to.
	BatchEstimatedUSD float64 `json:"batch_estimated_usd"`
}

// PromptView is an entry of the prompt history.
type PromptView struct {
	ID             string    `json:"prompt_id"`
	Prompt         string    `json:"prompt"`
	NegativePrompt string    `json:"negative_prompt,omitempty"`
	UseCount       int       `json:"use_count"`
	LastUsedAt     time.Time `json:"last_used_at"`
}

// Spend is the total of the history: what the results that made a file cost (the vendor's figure where it gave one, else the estimate).
type Spend struct {
	Items        int     `json:"items"`
	PricedUSD    float64 `json:"priced_usd"`
	UnknownItems int     `json:"unknown_items" jsonschema:"単価が分からず合計に入っていない本数"`
}
