package gentest

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
)

// registerMatch adds the tools of the prompt match (#916): from one reference image the LLM writes an English prompt, a model generates
// with it, the LLM measures how much the picture looks like the reference and fixes the prompt until it looks enough like it.
func registerMatch(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "estimate_prompt_match",
		"参考画像 1 枚から、画像モデルで似た絵が出るプロンプトを作る「プロンプトマッチ」の見積り。mode: auto（目標の似てる度に届くか max_rounds 回まで、プロンプトを直して生成し直す。既定）/ once（プロンプトを作って 1 回だけ生成）/ adjust（1 回ずつ。続きは continue_batch_id と instruction で）。見積りは「最大の回数 × モデルの単価」（ローカルは無料）に、LLM の呼び出し（プロンプトの作成・修正は match_llm、比較は match_llm_compare の行）をトークン単価で足したもの。単価が確かめられないモデル（ChatGPT のプランなど）の行は合計に入らず、警告 no_price が付く。上限（gentest.max_cost_usd）を超える見積りは over_limit が付き、run_prompt_match は始めない。run_prompt_match に estimate_id と total_usd をそのまま渡す / Price a prompt match without running it.",
		h.estimateMatch)
	mcpkit.Write(r, "run_prompt_match",
		"プロンプトマッチを実行する。estimate_prompt_match と同じ条件に、その estimate_id と confirmed_total_usd（= total_usd）を付ける。サーバーが見積りをやり直し、変わっていたり上限を超えたら conflict で拒否する。job_id と batch_id を返す。進捗は get_job、各回の結果（プロンプト・画像・似てる度・直したところ）は get_prompt_match、取り消しは cancel_generation_test。各回の画像は単発生成の履歴に残る（Take・コスト台帳には入らない）。参考画像はモデルには渡さず、LLM が見るだけ / Run a confirmed prompt match as a job.",
		h.runMatch, mcpkit.OpenWorld())
	mcpkit.Read(r, "get_prompt_match",
		"プロンプトマッチの状態と全ての回: 回ごとの英語のプロンプト・生成結果（result_id）・似てる度（0〜100）・残った違い・直したところ・日本語の指示・直す前のプロンプト。best_round / best_prompt は最も似ていた回で、そのプロンプトをそのまま使える（単発生成の prompts に渡す）。stop は終わった理由（reached 目標に届いた / limit 回数を使い切った / cancelled / error）/ A prompt match with every round.",
		h.getMatch)
	mcpkit.Write(r, "instruct_prompt_match",
		"実行中のプロンプトマッチに、言葉（日本語でよい）の指示を足す。次にプロンプトを直すときに取り込まれ、英語のプロンプトに反映される。終わったマッチには使えない（conflict）。続けるときは estimate_prompt_match / run_prompt_match に continue_batch_id と instruction を渡す / Add words for the next fix of a running match.",
		h.instructMatch)
}

// MatchReference is the reference image of a prompt match.
type MatchReference struct {
	Source    string `json:"source" jsonschema:"upload（create_upload_ticket purpose gentest_reference の reference_id）/ project_asset / reference_item（project_id が要る）/ history（単発生成の成功した画像結果の result_id）"`
	ID        string `json:"id" jsonschema:"source に応じた ID"`
	ProjectID string `json:"project_id,omitempty" jsonschema:"project_asset / reference_item のとき"`
}

// MatchInput is a prompt match: a new one (reference and model) or the go-on of one that is over (continue_batch_id).
type MatchInput struct {
	Mode            string          `json:"mode,omitempty" jsonschema:"auto（既定。目標に届くか max_rounds 回まで繰り返す）/ once（1 回だけ）/ adjust（1 回ずつ。結果を見て continue_batch_id で続ける）"`
	Reference       *MatchReference `json:"reference,omitempty" jsonschema:"参考にする画像 1 枚（新しいマッチに必要。continue_batch_id があるときは付けない）"`
	Model           string          `json:"model,omitempty" jsonschema:"画像を作るモデル（list_generation_models の画像モデルの id。新しいマッチに必要）"`
	ContinueBatchID string          `json:"continue_batch_id,omitempty" jsonschema:"終わったマッチの batch_id。その参考画像・モデル・縦横比のまま続ける。回の番号は続きから数える"`
	TargetScore     int             `json:"target_score,omitempty" jsonschema:"目標の似てる度（1〜100、既定 80。続けるときは省くと元のマッチの目標）"`
	MaxRounds       int             `json:"max_rounds,omitempty" jsonschema:"auto の上限の回数（1〜10、既定 5）。once / adjust は 1 回"`
	MaxCostUSD      float64         `json:"max_cost_usd,omitempty" jsonschema:"この実行で使ってよい費用の上限（USD。省略か 0 は models.json の gentest.max_cost_usd だけ）。見積り（最大の回数 × 単価）がこれを超えるなら始めない（over_limit）"`
	AspectRatio     string          `json:"aspect_ratio,omitempty" jsonschema:"16:9 のように。省くと 1:1。続けるときは付けない"`
	Instruction     string          `json:"instruction,omitempty" jsonschema:"言葉（日本語でよい）の指示。「もう少し明るく」「背景は海に」など。最初の回では参考画像から作るプロンプトに、続きでは最初の直しに取り込まれ、英語のプロンプトに反映される。1000 文字まで"`
}

func (in MatchInput) request() match.Request {
	r := match.Request{
		Mode: in.Mode, Model: in.Model, ContinueBatchID: in.ContinueBatchID, TargetScore: in.TargetScore, MaxRounds: in.MaxRounds,
		AspectRatio: in.AspectRatio, Instruction: in.Instruction, MaxCostUSD: in.MaxCostUSD,
	}
	if ref := in.Reference; ref != nil {
		r.Reference = gentest.RefInput{Source: ref.Source, ID: ref.ID, ProjectID: ref.ProjectID}
	}
	return r
}

func (h handlers) matcher() (*match.Service, error) {
	g, err := h.gt()
	if err != nil {
		return nil, err
	}
	m := match.From(g)
	if m == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "プロンプトマッチはこのアプリでは使えません", Status: 503}
	}
	return m, nil
}

func (h handlers) estimateMatch(ctx context.Context, in MatchInput) (match.Estimate, error) {
	m, err := h.matcher()
	if err != nil {
		return match.Estimate{}, err
	}
	e, err := m.Estimate(ctx, in.request())
	return e, mapError(err)
}

// MatchRunInput is a prompt match with the estimate that was confirmed.
type MatchRunInput struct {
	MatchInput
	EstimateID        string  `json:"estimate_id" jsonschema:"estimate_prompt_match の estimate_id"`
	ConfirmedTotalUSD float64 `json:"confirmed_total_usd" jsonschema:"estimate_prompt_match の total_usd。0 でも指定する"`
}

func (h handlers) runMatch(ctx context.Context, in MatchRunInput) (RunOutput, error) {
	m, err := h.matcher()
	if err != nil {
		return RunOutput{}, err
	}
	var ref gentest.RunRef
	jr, err := h.registrar.Jobs.Run(ctx, KindGentest, "", func(ctx context.Context) (string, error) {
		var err error
		ref, err = m.Run(ctx, in.request(), gentest.Confirmation{EstimateID: in.EstimateID, TotalUSD: in.ConfirmedTotalUSD})
		return ref.JobID, mapError(err)
	})
	return RunOutput{JobRef: jr, BatchID: ref.BatchID, TotalItems: ref.TotalItems}, err
}

func (h handlers) getMatch(ctx context.Context, in BatchInput) (match.View, error) {
	m, err := h.matcher()
	if err != nil {
		return match.View{}, err
	}
	v, err := m.View(ctx, in.BatchID)
	return v, mapError(err)
}

// InstructInput is words for a running match.
type InstructInput struct {
	BatchID     string `json:"batch_id" jsonschema:"run_prompt_match が返した batch_id"`
	Instruction string `json:"instruction" jsonschema:"言葉（日本語でよい）の指示。1000 文字まで"`
}

func (h handlers) instructMatch(ctx context.Context, in InstructInput) (map[string]string, error) {
	m, err := h.matcher()
	if err != nil {
		return nil, err
	}
	if err := m.Instruct(ctx, in.BatchID, in.Instruction); err != nil {
		return nil, mapError(err)
	}
	return map[string]string{"status": "次にプロンプトを直すときに取り込みます"}, nil
}

// running says the batch is a prompt match that has not ended: the generation test's own state calls it over as soon as the last
// picture is made, before the last similarity is measured and the rounds it did not need are removed.
func (h handlers) running(ctx context.Context, g *gentest.Service, batchID string) bool {
	m := match.From(g)
	if m == nil {
		return false
	}
	_, running, err := m.IsMatch(ctx, batchID)
	return err == nil && running
}
