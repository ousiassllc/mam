package gentest

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest/intentinfo"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/recipe"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"
)

// ListModelsInput narrows the models. The conditions are the ones every model picker has (modelfilter.Condition, #1011): kinds, inputs, adult,
// uncensored, params, vram_fit, commercial (as before: all / ok / ok_or_conditional, and a list of ok, conditional, unknown, ng), installed, origins,
// providers, audio, speeds, qualities, available_only and q.
type ListModelsInput struct {
	Media string `json:"media,omitempty" jsonschema:"image / video。省くと両方"`
	modelfilter.Condition
}

// ListModelsOutput is the models.
type ListModelsOutput struct {
	Models []modelcatalog.Info `json:"models"`
	// CommercialFilter is the filter that narrowed Models: what commercial asked for, or the setting's default.
	CommercialFilter string `json:"commercial_filter" jsonschema:"models に適用した商用利用の絞り込み（all / ok / ok_or_conditional。ok, conditional, unknown, ng のうち複数を指定したときはその一覧）"`
	// LoRAs are the LoRA files of the bundled ComfyUI engine (lora.name of a request; only for models with supports_lora). Empty
	// when the engine is not running or has none.
	LoRAs  []string `json:"loras" jsonschema:"同梱 ComfyUI エンジンにある LoRA ファイル名（昇順）。リクエストの lora.name に使う。supports_lora のモデルだけが使う。エンジンが止まっている・無いときは空"`
	Limits Limits   `json:"limits"`
	// Intents are the "やりたいこと" a request's intent takes, with the model each picks now (Issue #977).
	Intents []intentinfo.IntentInfo `json:"intents" jsonschema:"リクエストの intent に使えるやりたいこと（photo / anime / landscape / face）と、いま選ばれるモデル（導入済み・商用の絞り込み・OS・GPU の条件に合うもの）"`
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
	loras := g.LoRAs(ctx, in.Media)
	if loras == nil {
		loras = []string{}
	}
	models, applied, err := g.ModelsWhere(ctx, in.Media, in.Condition)
	if err != nil {
		return ListModelsOutput{}, mapError(err)
	}
	filter := applied.Commercial
	if filter == "" {
		filter = string(modellicense.FilterAll)
	}
	return ListModelsOutput{Models: models, CommercialFilter: filter, LoRAs: loras, Limits: Limits{MaxItems: items, MaxCostUSD: cost}, Intents: intentinfo.List(ctx, g)}, nil
}

// ReferenceInput is an input image of a batch.
type ReferenceInput struct {
	Source    string `json:"source" jsonschema:"upload（create_upload_ticket purpose gentest_reference の reference_id）/ project_asset / reference_item（project_id が要る）/ history（生成テストの成功した画像結果の result_id）/ pose_library（ポーズ集 Shisei の画像。id は std_P001_front のような画像 ID で、導入済みのときだけ。役割 pose と使う）/ batch_input（過去のバッチの入力画像。id は結果の詳細の inputs[].input_id。アップロードした画像を同じ条件で使い直すとき）"`
	ID        string `json:"id" jsonschema:"source に応じた ID"`
	ProjectID string `json:"project_id,omitempty" jsonschema:"project_asset / reference_item のとき"`
	Role      string `json:"role,omitempty" jsonschema:"画像: reference（全体。既定）/ face（顔だけ）/ style（画風・雰囲気だけ）/ edit（編集する元の画像）/ pose（ポーズだけ。ポーズ集 Shisei の画像や人形の写真。list_generation_models の reference_roles に pose があるモデルだけ）。動画: reference / first_frame / last_frame。list_generation_models の reference_roles に無い役割は警告 role_unsupported で全体の参照として使う"`
	// Strength is a pointer: omitted means the default (0.7), 0 is a deliberate "barely".
	Strength *float64 `json:"strength,omitempty" jsonschema:"画像モデルだけ。参照画像をどれだけ強く反映するか（0〜1、既定 0.7）。モデルごとの実現方法は隠す。動画では指定できない（validation）。強さを実現できるのは同梱 ComfyUI の参照対応モデルだけで、クラウドの画像モデルなどでは無視され、警告も出ない"`
}

// LoRAInput is a LoRA of the engine and its strength.
type LoRAInput struct {
	Name     string   `json:"name" jsonschema:"list_generation_models の loras のどれか"`
	Strength *float64 `json:"strength,omitempty" jsonschema:"0〜1、既定 0.4"`
}

// PromptInput is a prompt with its negative prompt.
type PromptInput struct {
	Prompt         string `json:"prompt" jsonschema:"1〜4000 文字。translate_to_english が true（既定）なら英語に訳して送る。false ならそのまま送る（補正しない）"`
	NegativePrompt string `json:"negative_prompt,omitempty" jsonschema:"ネガティブの欄が無いモデルには「Avoid: …」としてプロンプトに書き足して送る"`
}

// RequestInput is a batch: every model x every prompt x count.
type RequestInput struct {
	Media           string           `json:"media" jsonschema:"image / video"`
	Models          []string         `json:"models,omitempty" jsonschema:"list_generation_models の id。使えないモデルは拒否される。代替モデルへは切り替えない。intent を指定するときは省く（同時には指定できない）"`
	Prompts         []PromptInput    `json:"prompts" jsonschema:"1 件以上。同じ組は 1 件に数える"`
	Count           int              `json:"count,omitempty" jsonschema:"モデルとプロンプトの組ごとの本数（1〜20、既定 1）"`
	AspectRatio     string           `json:"aspect_ratio,omitempty" jsonschema:"16:9 のように。省くと画像は 1:1、動画は 16:9"`
	Resolution      string           `json:"resolution,omitempty" jsonschema:"動画だけ。モデルの resolutions のどれか。省くと 720p かモデルの最初のもの"`
	DurationSeconds float64          `json:"duration_seconds,omitempty" jsonschema:"動画だけ。省くと 5 秒。モデルが作れる長さに丸める"`
	References      []ReferenceInput `json:"references,omitempty" jsonschema:"入力画像（画像モデルの参照画像・動画の最初／最後のフレーム）。各画像に role と strength を付けられる。モデルが使える枚数は max_reference_images、役割は reference_roles（list_generation_models）。超えた分は警告 too_many_refs で使わない"`
	// AvoidReferencePose adds "same pose as reference, ..." to the negative prompt of every model that uses a reference.
	AvoidReferencePose bool `json:"avoid_reference_pose,omitempty" jsonschema:"画像だけ。参照画像のポーズ・構図を真似ないようにする（参照を使うモデルのネガティブプロンプトに「同じポーズ・構図」を足す。ネガティブの欄が無いモデルは Avoid: として書き足す。参照画像が無い・参照画像を使うモデルが無いときは何も起きず、警告も出ない）"`
	// LoRA is applied together with the references to every model that supports it.
	LoRA *LoRAInput `json:"lora,omitempty" jsonschema:"画像だけ。参照画像と一緒に使う LoRA。supports_lora でないモデルは警告 lora_unsupported で使わない"`
	// SpeedMode picks how a model samples (Info.SpeedModes).
	SpeedMode string `json:"speed_mode,omitempty" jsonschema:"速さのモード（蒸留 LoRA。list_generation_models のモデルの speed_modes の id。例: 動画の Wan 2.2 14B + LightX2V の fast = 4 ステップ・cfg 1 / standard = 通常の 20 ステップ・cfg 3.5、画像の Qwen-Image-Edit 2511 の standard = 20 ステップ・cfg 4 / lightning-8 = 8 ステップ・cfg 1 / lightning-4 = 4 ステップ・cfg 1）。モードがあるモデルのステップ数と cfg はモードが決める（sampling の steps・cfg は使わない）。省くと各モデルの既定（speed_modes の default）。そのモードを持たないモデルは警告 speed_mode_unsupported で既定のまま作る"`
	// Seed fixes the seed of every result (the n-th result of each model and prompt gets seed + n - 1).
	Seed *int64 `json:"seed,omitempty" jsonschema:"シードを固定する（0〜4294967295）。n 本目は seed + n − 1 を使う（本数が多いときは seed + 本数 − 1 もこの範囲）。モデルごとに同じ seed で比べられ、結果の seed からいつでも作り直せる。省くと結果ごとにランダム。シードを渡せないモデルは警告 seed_ignored で使わない"`
	// Sampling overrides the steps, cfg, sampler and scheduler of the models that take them.
	Sampling *sampling.Settings `json:"sampling,omitempty" jsonschema:"詳細設定: steps（1〜150）・cfg（0〜30）・sampler・scheduler。省いた項目は各モデルの既定（list_generation_models の sampling の default）。受け取らない項目（sampling に無い）と、速さのモードがあるモデルの steps・cfg（sampling の locked が speed_mode）は警告 sampling_ignored で使わない（速さのモードが優先）。結果の sampling に使った値が残る"`
	// Quality picks the steps and the cfg of every model from its preset (sampling.quality_presets); Intent leaves the choice of the model to mam.
	Quality string `json:"quality,omitempty" jsonschema:"画像だけ。品質: fast（速い）/ standard（標準。省略と同じ: モデルの既定の値）/ high（きれい）。モデルの sampling.quality_presets にあるステップ数・CFG で作る（sampling に steps・cfg を書けばそちらが優先）。プリセットの無いモデルは警告 quality_ignored で標準の値で作る"`
	Intent  string `json:"intent,omitempty" jsonschema:"画像だけ。やりたいこと: photo（人物写真）/ anime（アニメ）/ landscape（風景）/ face（同じ顔で。references に顔の写真が要る）。導入済みで使えるモデルを mam が選ぶ（models は省く。list_generation_models の intents に今選ばれるモデルが出る）。合うモデルが無いときは理由つきで validation"`
	// Recipe takes the model and the settings of a saved recipe (list_generation_recipes); Exposed gives the values of the models' exposed inputs.
	Recipe  string         `json:"recipe,omitempty" jsonschema:"画像だけ。保存したレシピの id（list_generation_recipes）。レシピのモデルと設定（ステップ数・CFG・サンプラー・スケジューラー・シード・ネガティブ・LoRA・入力画像の強さ・サイズ・品質・画面に出す値）で作る。models・intent とは同時に指定できない。このリクエストで名指しした項目（sampling・seed・exposed など）が勝つ。使えないレシピ（モデルの削除・未導入・成人ゲート・商用の絞り込み）は理由つきで validation"`
	Exposed map[string]any `json:"exposed,omitempty" jsonschema:"画像だけ。モデルの画面に出す値（list_generation_models の exposed）の値。キーは <ノード ID>.<入力名>。型（int / float / string / enum）と範囲・選択肢の外は validation。どのモデルにも無いキーも validation。そのキーを持たないモデルは警告 exposed_ignored。結果の exposed に使った値が残る"`
	// Mask is the inpaint mask (same shape as a reference; white redraws). Every model must support inpaint. Denoise nil uses denoise_default.
	Mask    *ReferenceInput `json:"mask,omitempty" jsonschema:"一部だけ描き直すときのマスク。references と同じ形式（source / id / project_id。role と strength は使わない）。白が描き直す部分、黒が残す部分。元画像は references に 1 枚だけ。supports_inpaint でないモデルは validation で拒否する"`
	Denoise *float64        `json:"denoise,omitempty" jsonschema:"描き直しの強さ（0 より大きく 1 以下）。省くと各モデルの denoise_default。マスクが無いときは指定できない"`
	// TranslateToEnglish is a pointer: omitted means the default of the screen (on), false is a deliberate off.
	TranslateToEnglish *bool `json:"translate_to_english,omitempty" jsonschema:"プロンプトとネガティブプロンプトを LLM（LLM の割り当ての Prompt Planner）で英語に訳してから送る。既定 true（画面と同じ）。すでに英語の文は訳さずそのまま。訳せなければ何も生成せずエラー（upstream）。見積りには訳す文の数が単価なしの行（model が translate）として入る。false でそのまま送る"`
}

func (in RequestInput) request() gentest.Request {
	r := gentest.Request{
		Media: in.Media, Models: in.Models, Count: in.Count, AspectRatio: in.AspectRatio, Resolution: in.Resolution,
		DurationSeconds: in.DurationSeconds, TranslateToEnglish: gentest.DefaultTranslate, AvoidReferencePose: in.AvoidReferencePose, SpeedMode: in.SpeedMode,
		Quality: sampling.Quality(in.Quality),
	}
	if in.LoRA != nil {
		r.LoRA = &gentest.LoRAInput{Name: in.LoRA.Name, Strength: in.LoRA.Strength}
	}
	if in.TranslateToEnglish != nil {
		r.TranslateToEnglish = *in.TranslateToEnglish
	}
	r.Seed = in.Seed
	if in.Sampling != nil {
		r.Sampling = *in.Sampling
	}
	for _, p := range in.Prompts {
		r.Prompts = append(r.Prompts, gentest.PromptInput{Prompt: p.Prompt, Negative: p.NegativePrompt})
	}
	for _, ref := range in.References {
		r.References = append(r.References, gentest.RefInput{Source: ref.Source, ID: ref.ID, ProjectID: ref.ProjectID, Role: ref.Role, Strength: ref.Strength})
	}
	if in.Mask != nil {
		r.Mask = &gentest.RefInput{Source: in.Mask.Source, ID: in.Mask.ID, ProjectID: in.Mask.ProjectID}
	}
	r.Denoise = in.Denoise
	r.Exposed = in.Exposed
	return r
}

// resolve is the request of the input, with the model the intent picks when it names one.
func (h handlers) resolve(ctx context.Context, g *gentest.Service, in RequestInput) (gentest.Request, error) {
	r := in.request()
	if in.Recipe != "" {
		if in.Intent != "" {
			return r, mapError(gterr.Invalid("recipe と intent は同時に指定できません"))
		}
		r, _, err := recipe.New(g).Apply(ctx, r, in.Recipe)
		return r, mapError(err)
	}
	if in.Intent == "" {
		return r, nil
	}
	r, _, err := g.ApplyIntent(ctx, r, in.Intent)
	return r, mapError(err)
}

func (h handlers) estimate(ctx context.Context, in RequestInput) (gentest.Estimate, error) {
	g, err := h.gt()
	if err != nil {
		return gentest.Estimate{}, err
	}
	r, err := h.resolve(ctx, g, in)
	if err != nil {
		return gentest.Estimate{}, err
	}
	e, err := g.Estimate(ctx, r)
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
	req, err := h.resolve(ctx, g, in.RequestInput)
	if err != nil {
		return RunOutput{}, err
	}
	var ref gentest.RunRef
	jr, err := h.registrar.Jobs.Run(ctx, KindGentest, "", func(ctx context.Context) (string, error) {
		var err error
		ref, err = g.Run(ctx, req, gentest.Confirmation{EstimateID: in.EstimateID, TotalUSD: in.ConfirmedTotalUSD})
		return ref.JobID, mapError(err)
	})
	return RunOutput{JobRef: jr, BatchID: ref.BatchID, TotalItems: ref.TotalItems}, err
}

// BatchInput names a batch.
type BatchInput struct {
	BatchID string `json:"batch_id" jsonschema:"run_generation_test が返した batch_id"`
}

// BatchOutput is the batch with, while it runs, how far the ComfyUI call that makes its results is (Issue #966).
type BatchOutput struct {
	gentest.BatchStatus
	Progress *live.View `json:"progress,omitempty" jsonschema:"いま動いているローカル ComfyUI の生成の進み具合（ノード・ステップ・%・経過・残り時間）。取れないとき（クラウドのモデル・準備中・終わったバッチ）は省略 / step, percent and time left of the local ComfyUI call that is running; left out when unknown"`
}

func (h handlers) get(ctx context.Context, in BatchInput) (BatchOutput, error) {
	g, err := h.gt()
	if err != nil {
		return BatchOutput{}, err
	}
	b, err := g.Batch(ctx, in.BatchID, true)
	if err != nil {
		return BatchOutput{}, mapError(err)
	}
	if h.running(ctx, g, in.BatchID) {
		b.State, b.Finished = gentest.StateRunning, false
	}
	return BatchOutput{BatchStatus: b, Progress: stepsOf(b)}, nil
}

func (h handlers) cancel(ctx context.Context, in BatchInput) (runner.CancelResult, error) {
	g, err := h.gt()
	if err != nil {
		return runner.CancelResult{}, err
	}
	res, err := g.Cancel(ctx, in.BatchID)
	return res, mapError(err)
}
