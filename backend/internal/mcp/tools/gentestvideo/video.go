// Package gentestvideo holds the MCP tools of "make a video larger" and "make a video smoother" of the generation test (Issue #1031,
// spec/api/mcp-tools-generation-test.md): the models that do it (local SeedVR2 and RIFE, and the paid cloud Topaz Video API), the estimate of the
// cloud and the start of an enhancement. The progress and the result are read with get_job / get_generation_test / get_generation_result like any
// result (the field video_enhance). It is a package of its own because tools/gentest is at its line limit.
package gentestvideo

import (
	"context"
	"errors"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
)

// Register adds the tools. They answer "unavailable" when the app has no generation test.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc, registrar: r}
	mcpkit.Read(r, "list_generation_video_enhancers",
		"生成テストの動画の結果を大きくする（アップスケール）・コマを増やして滑らかにする（フレーム補間）モデルの一覧。ローカル（ComfyUI の SeedVR2 3B と RIFE。無料。モデルマネージャーで入れたものだけが出る）と、"+
			"有料クラウドの Topaz Video API（API キーが無いときは available=false で、unavailable_message に直し方が出る）。provider・paid・商用可否（license）・表示が必要な出典（attribution）つき。"+
			"ローカルの SeedVR2 / RIFE は RTX 4070 Ti SUPER 16GB での動作は実機未確認。Topaz は有料クラウド（見積り→確認が必須）で、表示には「Powered by Topaz Labs」が要る。"+
			"大きくする倍率の上限は max_scale、コマ補間の倍率は multipliers、Topaz の fps の範囲は cloud_fps_range、この PC（ローカル）の出力 fps の上限は local_max_fps（120）、出力の 1 コマの画素数の上限は max_output_pixels で返る。",
		h.list)
	mcpkit.Read(r, "estimate_generation_video_enhance",
		"動画の結果を大きくする・コマを増やす処理の見積り。元の動画の大きさ・fps・コマ数は ffprobe でファイルから読む（ffmpeg が無いと日本語で直し方を返す）。"+
			"ローカルのモデルは無料で確認不要（estimate_id なし）。Topaz は Topaz に見積りを聞いて credits を返し（課金されない・クレジットは円やドルに換算しない）、estimate_id を返す（30 分有効・1 回だけ使える）。"+
			"見積りが取れなければ実行できない。op は upscale（scale か target_short_side）か interpolate（multiplier か target_fps）。"+
			"ローカルのコマ補間は元の fps の 2 倍・4 倍だけで、出力は 120 fps まで（それ以外の target_fps や、120 fps を超える倍率は断る。任意の fps や 240 fps までは Topaz で 15〜240 fps）。",
		h.estimate, mcpkit.OpenWorld())
	mcpkit.Write(r, "enhance_generation_video",
		"生成テストの成功した動画の結果を大きくする（op=upscale。scale は 1 より大きく 4 以下、または target_short_side で短い辺を 720 / 1080 などに）か、コマを増やして滑らかにする（op=interpolate。multiplier は 2 か 4、または target_fps）。"+
			"結果は元とは別の結果（別ファイル）として履歴に残り、get_generation_result の video_enhance に元の result_id・操作・モデル・倍率や fps が入る。大きくした結果をもう一度処理してもよい（拡大のあとに補間、など）。"+
			"アップスケールとコマ補間は別々の処理で、1 回に 1 つ。進捗は返る job_id を get_job / get_generation_test で読む。"+
			"ローカルは無料でそのまま実行できる。Topaz（model が topaz:...）は有料クラウドで、estimate_generation_video_enhance の estimate_id と、利用者が確認した confirmed_credits（見積りの credits と同じ値）が無いと実行しない。"+
			"Topaz が受け付けたあとに失敗しても自動では再送しない（二重課金を避ける）。表示には「Powered by Topaz Labs」が要る。",
		h.enhance, mcpkit.OpenWorld())
}

type handlers struct {
	svc       *service.Services
	registrar *mcpkit.Registrar
}

// Input is a tool without arguments.
type Input struct{}

func (h handlers) gt() (*gentest.Service, error) {
	if h.svc.Gentest == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "生成テストはこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.Gentest, nil
}

// mapError turns the refusals of the generation test into the codes a tool answers with.
func mapError(err error) error {
	var (
		invalid  *gterr.InvalidError
		conflict *gterr.ConflictError
	)
	switch {
	case err == nil:
		return nil
	case errors.As(err, &invalid):
		return &mcpkit.ToolError{Code: mcpkit.CodeValidation, Message: invalid.Message, Status: http.StatusUnprocessableEntity}
	case errors.As(err, &conflict):
		return &mcpkit.ToolError{Code: mcpkit.CodeConflict, Message: conflict.Message, Status: http.StatusConflict}
	case errors.Is(err, gterr.ErrNotFound):
		return &mcpkit.ToolError{Code: mcpkit.CodeNotFound, Message: "対象が見つかりません", Status: http.StatusNotFound}
	case errors.Is(err, gterr.ErrUnavailable):
		return &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "生成テストはこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return mcpkit.MapError(err)
}

// EnhancersOutput is the models that make a video larger or smoother.
type EnhancersOutput struct {
	Enhancers []modelcatalog.VideoEnhancer `json:"enhancers"`
	// MaxScale, Multipliers and the limits are what the tools take.
	MaxScale        float64    `json:"max_scale" jsonschema:"scale の上限（1 より大きく、この値まで）"`
	Multipliers     []int      `json:"multipliers" jsonschema:"multiplier に渡せる値（ローカルのコマ補間は元の fps のこの倍数だけ）"`
	CloudFPSRange   [2]float64 `json:"cloud_fps_range" jsonschema:"Topaz のコマ補間で指定できる fps の範囲（下限・上限）"`
	LocalMaxFPS     float64    `json:"local_max_fps" jsonschema:"この PC（ローカル）のコマ補間で出力できる fps の上限（元の fps × multiplier がこれを超えると断る。超えるときは Topaz を使う）"`
	MaxOutputPixels int        `json:"max_output_pixels" jsonschema:"出力の 1 コマの画素数の上限（幅 × 高さ。モデルごとにもっと低い max_output_pixels がある）"`
}

func (h handlers) list(ctx context.Context, _ Input) (EnhancersOutput, error) {
	g, err := h.gt()
	if err != nil {
		return EnhancersOutput{}, err
	}
	list := videoenhance.Enhancers(ctx, g)
	if list == nil {
		list = []modelcatalog.VideoEnhancer{}
	}
	return EnhancersOutput{
		Enhancers: list, MaxScale: venhance.MaxScale, Multipliers: venhance.Multipliers,
		CloudFPSRange: [2]float64{venhance.MinCloudFPS, venhance.MaxCloudFPS}, LocalMaxFPS: venhance.MaxLocalFPS, MaxOutputPixels: venhance.MaxOutputPixels,
	}, nil
}

// EnhanceInput is one enhancement (and its estimate).
type EnhanceInput struct {
	ResultID        string  `json:"result_id" jsonschema:"処理する結果の result_id（成功した動画の結果。処理した結果をもう一度処理してもよい）"`
	Op              string  `json:"op" jsonschema:"upscale（大きくする）か interpolate（コマを増やす）"`
	Model           string  `json:"model,omitempty" jsonschema:"list_generation_video_enhancers の id。省くと、op に合う最初のローカルのモデル（有料のものは自動では選ばない）"`
	Scale           float64 `json:"scale,omitempty" jsonschema:"upscale: 倍率。1 より大きく 4 以下（2 が標準）。target_short_side とどちらか一方"`
	TargetShortSide int     `json:"target_short_side,omitempty" jsonschema:"upscale: 短い辺をこの長さにする（720 / 1080 など。480×848 を 720 にすると 720×1272）。scale とどちらか一方"`
	Multiplier      int     `json:"multiplier,omitempty" jsonschema:"interpolate: コマ数の倍率（2 か 4）。target_fps とどちらか一方"`
	TargetFPS       float64 `json:"target_fps,omitempty" jsonschema:"interpolate: 出力の fps。ローカルは元の fps の 2 倍か 4 倍だけで、出力は 120 fps まで。Topaz は 15〜240。multiplier とどちらか一方"`
}

func (in EnhanceInput) request() videoenhance.Request {
	return videoenhance.Request{
		ResultID: in.ResultID, Op: cloudenhance.Op(in.Op), Model: in.Model, Scale: in.Scale, TargetShortSide: in.TargetShortSide, Multiplier: in.Multiplier, TargetFPS: in.TargetFPS,
	}
}

func (h handlers) estimate(ctx context.Context, in EnhanceInput) (videoenhance.Estimate, error) {
	g, err := h.gt()
	if err != nil {
		return videoenhance.Estimate{}, err
	}
	est, err := videoenhance.EstimateEnhance(ctx, g, in.request())
	return est, mapError(err)
}

// RunInput is one enhancement to start: the values of the estimate, and for a cloud model the quote and the credits the user confirmed.
type RunInput struct {
	ResultID         string  `json:"result_id" jsonschema:"処理する結果の result_id（成功した動画の結果）"`
	Op               string  `json:"op" jsonschema:"upscale（大きくする）か interpolate（コマを増やす）"`
	Model            string  `json:"model,omitempty" jsonschema:"list_generation_video_enhancers の id。省くと、op に合う最初のローカルのモデル"`
	Scale            float64 `json:"scale,omitempty" jsonschema:"upscale: 倍率。1 より大きく 4 以下。target_short_side とどちらか一方"`
	TargetShortSide  int     `json:"target_short_side,omitempty" jsonschema:"upscale: 短い辺をこの長さにする（720 / 1080 など）。scale とどちらか一方"`
	Multiplier       int     `json:"multiplier,omitempty" jsonschema:"interpolate: コマ数の倍率（2 か 4）。target_fps とどちらか一方"`
	TargetFPS        float64 `json:"target_fps,omitempty" jsonschema:"interpolate: 出力の fps。multiplier とどちらか一方"`
	EstimateID       string  `json:"estimate_id,omitempty" jsonschema:"estimate_generation_video_enhance の estimate_id（Topaz などの有料クラウドは必須。ローカルは不要）"`
	ConfirmedCredits float64 `json:"confirmed_credits,omitempty" jsonschema:"利用者が確認したクレジット（見積りの credits と同じ値。有料クラウドは必須）"`
}

// RunOutput is a started enhancement.
type RunOutput struct {
	mcpkit.JobRef
	BatchID  string `json:"batch_id"`
	ResultID string `json:"result_id" jsonschema:"作った結果の result_id（終わるまでは status が queued / running）"`
}

func (h handlers) enhance(ctx context.Context, in RunInput) (RunOutput, error) {
	g, err := h.gt()
	if err != nil {
		return RunOutput{}, err
	}
	req := videoenhance.Request{
		ResultID: in.ResultID, Op: cloudenhance.Op(in.Op), Model: in.Model, Scale: in.Scale, TargetShortSide: in.TargetShortSide, Multiplier: in.Multiplier, TargetFPS: in.TargetFPS,
		EstimateID: in.EstimateID, ConfirmedCredits: in.ConfirmedCredits,
	}
	var ref videoenhance.Ref
	jr, err := h.registrar.Jobs.Run(ctx, mcpgentest.KindGentest, "", func(ctx context.Context) (string, error) {
		var err error
		ref, err = videoenhance.Enhance(ctx, g, req)
		return ref.JobID, mapError(err)
	})
	return RunOutput{JobRef: jr, BatchID: ref.BatchID, ResultID: ref.ResultID}, err
}
