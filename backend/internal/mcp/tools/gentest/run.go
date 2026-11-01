package gentest

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// ListModelsInput narrows the models.
type ListModelsInput struct {
	Media string `json:"media,omitempty" jsonschema:"image / video。省くと両方"`
}

// ListModelsOutput is the models.
type ListModelsOutput struct {
	Models []modelcatalog.Info `json:"models"`
	Limits Limits              `json:"limits"`
}

// Limits are the caps of one batch (models.json: gentest).
type Limits struct {
	MaxItems   int     `json:"max_items"`
	MaxCostUSD float64 `json:"max_cost_usd" jsonschema:"0 は上限なし"`
}

func (h handlers) listModels(ctx context.Context, in ListModelsInput) (ListModelsOutput, error) {
	g, err := h.gt()
	if err != nil {
		return ListModelsOutput{}, err
	}
	items, cost := g.Limits()
	return ListModelsOutput{Models: g.Models(ctx, in.Media), Limits: Limits{MaxItems: items, MaxCostUSD: cost}}, nil
}

// ReferenceInput is an input image of a batch.
type ReferenceInput struct {
	Source    string `json:"source" jsonschema:"upload（create_upload_ticket purpose gentest_reference の reference_id）/ project_asset / reference_item（project_id が要る）/ history（生成テストの成功した画像結果の result_id）"`
	ID        string `json:"id" jsonschema:"source に応じた ID"`
	ProjectID string `json:"project_id,omitempty" jsonschema:"project_asset / reference_item のとき"`
	Role      string `json:"role,omitempty" jsonschema:"reference（既定）/ first_frame / last_frame（動画のみ）。モデルが使えない役割は警告つきでそのモデルから外す"`
}

// PromptInput is a prompt with its negative prompt.
type PromptInput struct {
	Prompt         string `json:"prompt" jsonschema:"1〜4000 文字。translate_to_english が true（既定）なら英語に訳して送る。false ならそのまま送る（補正しない）"`
	NegativePrompt string `json:"negative_prompt,omitempty" jsonschema:"ネガティブの欄が無いモデルには「Avoid: …」としてプロンプトに書き足して送る"`
}

// RequestInput is a batch: every model x every prompt x count.
type RequestInput struct {
	Media           string           `json:"media" jsonschema:"image / video"`
	Models          []string         `json:"models" jsonschema:"list_generation_models の id。使えないモデルは拒否される。代替モデルへは切り替えない"`
	Prompts         []PromptInput    `json:"prompts" jsonschema:"1 件以上。同じ組は 1 件に数える"`
	Count           int              `json:"count,omitempty" jsonschema:"モデルとプロンプトの組ごとの本数（1〜20、既定 1）"`
	AspectRatio     string           `json:"aspect_ratio,omitempty" jsonschema:"16:9 のように。省くと画像は 1:1、動画は 16:9"`
	Resolution      string           `json:"resolution,omitempty" jsonschema:"動画だけ。モデルの resolutions のどれか。省くと 720p かモデルの最初のもの"`
	DurationSeconds float64          `json:"duration_seconds,omitempty" jsonschema:"動画だけ。省くと 5 秒。モデルが作れる長さに丸める"`
	References      []ReferenceInput `json:"references,omitempty" jsonschema:"入力画像（画像モデルの参照画像・動画の最初／最後のフレーム）"`
	// TranslateToEnglish is a pointer: omitted means the default of the screen (on), false is a deliberate off.
	TranslateToEnglish *bool `json:"translate_to_english,omitempty" jsonschema:"プロンプトとネガティブプロンプトを LLM（LLM の割り当ての Prompt Planner）で英語に訳してから送る。既定 true（画面と同じ）。すでに英語の文は訳さずそのまま。訳せなければ何も生成せずエラー（upstream）。見積りには訳す文の数が単価なしの行（model が translate）として入る。false でそのまま送る"`
}

func (in RequestInput) request() gentest.Request {
	r := gentest.Request{
		Media: in.Media, Models: in.Models, Count: in.Count, AspectRatio: in.AspectRatio, Resolution: in.Resolution,
		DurationSeconds: in.DurationSeconds, TranslateToEnglish: gentest.DefaultTranslate,
	}
	if in.TranslateToEnglish != nil {
		r.TranslateToEnglish = *in.TranslateToEnglish
	}
	for _, p := range in.Prompts {
		r.Prompts = append(r.Prompts, gentest.PromptInput{Prompt: p.Prompt, Negative: p.NegativePrompt})
	}
	for _, ref := range in.References {
		r.References = append(r.References, gentest.RefInput{Source: ref.Source, ID: ref.ID, ProjectID: ref.ProjectID, Role: ref.Role})
	}
	return r
}

func (h handlers) estimate(ctx context.Context, in RequestInput) (gentest.Estimate, error) {
	g, err := h.gt()
	if err != nil {
		return gentest.Estimate{}, err
	}
	e, err := g.Estimate(ctx, in.request())
	return e, mapError(err)
}

// RunInput is a batch with the estimate that was confirmed.
type RunInput struct {
	RequestInput
	EstimateID        string  `json:"estimate_id" jsonschema:"estimate_generation_test の estimate_id"`
	ConfirmedTotalUSD float64 `json:"confirmed_total_usd" jsonschema:"estimate_generation_test の total_usd（単価不明のモデルは含まない）。0 でも指定する"`
}

// RunOutput is the batch that was queued.
type RunOutput struct {
	mcpkit.JobRef
	BatchID    string `json:"batch_id" jsonschema:"get_generation_test などに渡す"`
	TotalItems int    `json:"total_items"`
}

func (h handlers) run(ctx context.Context, in RunInput) (RunOutput, error) {
	g, err := h.gt()
	if err != nil {
		return RunOutput{}, err
	}
	var ref gentest.RunRef
	jr, err := h.registrar.Jobs.Run(ctx, KindGentest, "", func(ctx context.Context) (string, error) {
		var err error
		ref, err = g.Run(ctx, in.request(), gentest.Confirmation{EstimateID: in.EstimateID, TotalUSD: in.ConfirmedTotalUSD})
		return ref.JobID, mapError(err)
	})
	return RunOutput{JobRef: jr, BatchID: ref.BatchID, TotalItems: ref.TotalItems}, err
}

// BatchInput names a batch.
type BatchInput struct {
	BatchID string `json:"batch_id" jsonschema:"run_generation_test が返した batch_id"`
}

func (h handlers) get(ctx context.Context, in BatchInput) (gentest.BatchStatus, error) {
	g, err := h.gt()
	if err != nil {
		return gentest.BatchStatus{}, err
	}
	b, err := g.Batch(ctx, in.BatchID, true)
	return b, mapError(err)
}

func (h handlers) cancel(ctx context.Context, in BatchInput) (gentest.CancelResult, error) {
	g, err := h.gt()
	if err != nil {
		return gentest.CancelResult{}, err
	}
	res, err := g.Cancel(ctx, in.BatchID)
	return res, mapError(err)
}
