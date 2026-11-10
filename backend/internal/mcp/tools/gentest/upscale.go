package gentest

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/upscale"
)

// registerUpscale adds the tools of "make it larger" (#975): the upscale workflows installed, and the enlargement of a result.
func registerUpscale(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "list_generation_upscalers",
		"単発生成の結果を大きくする（アップスケール）ワークフローの一覧。モデルマネージャーで入れたアップスケール用モデルのものだけが出る（入っていなければ空）。mode は model（アップスケール用モデルだけ）/ two_pass（そのあと低い denoise でもう一度サンプリングして仕上げる。元の結果のプロンプトとシードを引き継ぐ）。各モデルの license に商用利用の可否（commercial: yes / conditional / no / unknown）がある。id を upscale_generation_result の model に渡す / The upscale workflows that are installed.",
		h.listUpscalers)
	mcpkit.Write(r, "upscale_generation_result",
		"単発生成の成功した画像の結果を 2 倍か 4 倍に大きくする。結果は元とは別の結果（別ファイル）として履歴に残り、get_generation_result の upscale に元の result_id・倍率・使ったモデルが入る。ローカルのモデルなので見積り・確認は要らず、すぐ始まる。model を省くと、入っているアップスケール用モデルのうち、プロンプトの要らないもの（mode が model）の最初を使う。16GB の GPU での 4 倍の動作は実機で未確認。job_id・batch_id・result_id を返す。進みは get_job / get_generation_test / Take・コスト台帳には入らない / Enlarge a succeeded image result 2x or 4x as a result of its own.",
		h.upscale)
}

// UpscalersOutput is the upscale workflows installed.
type UpscalersOutput struct {
	Upscalers []modelcatalog.Upscaler `json:"upscalers"`
	Scales    []int                   `json:"scales" jsonschema:"upscale_generation_result の scale に渡せる倍率"`
	// MaxOutputPixels is the most pixels (width × height) an enlargement has (a workflow may have a lower limit: max_output_pixels).
	MaxOutputPixels int `json:"max_output_pixels" jsonschema:"大きくした結果の画素数の上限（幅 × 高さ。モデルごとにもっと低い max_output_pixels がある）"`
}

func (h handlers) listUpscalers(ctx context.Context, _ Input) (UpscalersOutput, error) {
	g, err := h.gt()
	if err != nil {
		return UpscalersOutput{}, err
	}
	list := g.Upscalers(ctx)
	if list == nil {
		list = []modelcatalog.Upscaler{}
	}
	return UpscalersOutput{Upscalers: list, Scales: upscale.Scales, MaxOutputPixels: upscale.MaxOutputPixels}, nil
}

// UpscaleInput is one enlargement.
type UpscaleInput struct {
	ResultID string `json:"result_id" jsonschema:"大きくする結果の result_id（成功した画像の結果。大きくした結果をもう一度大きくしてもよい）"`
	Scale    int    `json:"scale" jsonschema:"倍率。2 か 4"`
	Model    string `json:"model,omitempty" jsonschema:"list_generation_upscalers の id。省くと、プロンプトの要らないもの（mode が model）の最初"`
}

// UpscaleOutput is a started enlargement.
type UpscaleOutput struct {
	mcpkit.JobRef
	BatchID  string `json:"batch_id"`
	ResultID string `json:"result_id" jsonschema:"大きくした結果の result_id"`
}

func (h handlers) upscale(ctx context.Context, in UpscaleInput) (UpscaleOutput, error) {
	g, err := h.gt()
	if err != nil {
		return UpscaleOutput{}, err
	}
	var ref upscale.Ref
	jr, err := h.registrar.Jobs.Run(ctx, KindGentest, "", func(ctx context.Context) (string, error) {
		var err error
		ref, err = upscale.Run(ctx, g, upscale.Request{ResultID: in.ResultID, Scale: in.Scale, Model: in.Model})
		return ref.JobID, mapError(err)
	})
	return UpscaleOutput{JobRef: jr, BatchID: ref.BatchID, ResultID: ref.ResultID}, err
}
