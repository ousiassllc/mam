package videoenhance

import (
	"context"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
)

// Output is the video an enhancement makes, as planned (the model rounds the sides: the file is what is read back).
type Output struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	FPS    float64 `json:"fps"`
}

// SourceFacts are the facts of the source video as ffprobe reads them from the file.
type SourceFacts struct {
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	FPS        float64 `json:"fps"`
	FrameCount int     `json:"frame_count"`
	Duration   float64 `json:"duration_seconds"`
	HasAudio   bool    `json:"has_audio"`
}

// Estimate is what an enhancement would do and cost. A local model is free and has no EstimateID; a cloud model has the quote of the provider
// (Credits are the credits of the provider, never converted to a price) and the EstimateID to confirm it with.
type Estimate struct {
	Model    string      `json:"model"`
	Op       string      `json:"op"`
	Provider string      `json:"provider"`
	Paid     bool        `json:"paid"`
	Source   SourceFacts `json:"source"`
	Output   Output      `json:"output"`
	// EstimateID and ConfirmedCredits are for enhance_generation_video (a cloud model only).
	EstimateID string `json:"estimate_id,omitempty" jsonschema:"見積りの ID（有料クラウドのみ。enhance_generation_video の estimate_id に渡す）"`
	// Credits is the quote (the upper bound when the provider gives a range); CreditsMin the lower bound.
	Credits    *float64   `json:"credits,omitempty" jsonschema:"見積りのクレジット（上限）。enhance_generation_video の confirmed_credits にこの値を渡して確認する。クレジット単価はプランで違うので円・ドルには換算しない"`
	CreditsMin *float64   `json:"credits_min,omitempty" jsonschema:"見積りのクレジット（下限。範囲で返ってきたときだけ）"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty" jsonschema:"この見積りで実行できる期限（過ぎたら見積りを取り直す）"`
	// Adult and AdultWarning are set when the source came from an adult model and the model is the paid cloud: the video would go to an external
	// cloud. The run needs adult_confirmed = true (the user has read the warning).
	Adult        bool   `json:"adult,omitempty" jsonschema:"元の動画が成人向けのモデルで作られたものか（Topaz の見積りだけ）。true のとき adult_warning を利用者に見せ、確認が取れたら enhance_generation_video に adult_confirmed=true を渡す"`
	AdultWarning string `json:"adult_warning,omitempty" jsonschema:"成人向けの動画を外部のクラウド（Topaz Labs）に送る前の警告。利用者に見せて確認を取る（確認なしでは送らない）"`
	// Message says what to do next.
	Message     string `json:"message"`
	Attribution string `json:"attribution,omitempty" jsonschema:"表示が必要な出典（Topaz は Powered by Topaz Labs）"`
}

func factsOf(s cloudenhance.Source) SourceFacts {
	return SourceFacts{Width: s.Width, Height: s.Height, FPS: s.FPS, FrameCount: s.FrameCount, Duration: s.Duration, HasAudio: s.HasAudio}
}

// EstimateEnhance says what an enhancement would make and cost. A local model is free (nothing to confirm). For a cloud model it asks the provider for
// the quote (nothing is charged by it), keeps it for venhance.EstimateTTL and returns its id: if the quote cannot be had, there is no estimate and
// the enhancement cannot run.
func EstimateEnhance(ctx context.Context, g *gentest.Service, r Request) (Estimate, error) {
	t, err := prepare(ctx, g, r)
	if err != nil {
		return Estimate{}, err
	}
	est := Estimate{
		Model: t.model.ID, Op: t.model.Op, Provider: t.model.Provider, Paid: t.model.Paid, Source: factsOf(t.facts),
		Output: Output{Width: t.plan.Width, Height: t.plan.Height, FPS: t.plan.FPS},
	}
	if !t.model.Paid {
		est.Message = "ローカルのモデルなので無料です。見積りも確認も要りません。そのまま enhance_generation_video で実行できます（estimate_id は要りません）"
		return est, nil
	}
	q, err := g.Cloud.Estimate(ctx, cloudenhance.CloudRequest{Model: strings.TrimPrefix(t.model.ID, CloudPrefix), Source: t.facts, Params: r.params()})
	if err != nil {
		return Estimate{}, errmap.Report(g.Logger, "video enhancement estimate failed with an unknown error", err, "model", t.model.ID)
	}
	if q.Width > 0 && q.Height > 0 {
		est.Output.Width, est.Output.Height = q.Width, q.Height
	}
	if q.FPS > 0 {
		est.Output.FPS = q.FPS
	}
	id, expires := g.VideoEstimates.Put(venhance.Quote{ResultID: r.ResultID, Model: t.model.ID, Plan: t.plan, Estimate: q})
	est.EstimateID, est.Credits, est.ExpiresAt, est.Attribution = id, &q.Credits, &expires, venhance.TopazAttribution
	if q.CreditsMin > 0 && q.CreditsMin < q.Credits {
		est.CreditsMin = &q.CreditsMin
	}
	est.Message = "Topaz の有料クラウドです。実行するとクレジットを使います。利用者がこの額でよいと確認したら、enhance_generation_video に estimate_id と confirmed_credits（= credits）を渡してください。" +
		"見積りは 30 分で切れ、1 回しか使えません。実行して Topaz が受け付けたあとに失敗しても、自動では再送しません（二重課金を避けるため）"
	if t.adult {
		est.Adult, est.AdultWarning = true, venhance.AdultWarning
		est.Message += "。この動画は成人向けのモデルで作ったものです。adult_warning を利用者に見せ、読んだうえで確認が取れたときだけ adult_confirmed=true も渡してください（なければ送りません）"
	}
	return est, nil
}

// errExpired is the refusal of a quote that is unknown, over or used.
func errExpired() error {
	return gterr.Conflict("見積りの有効期限が切れました。もう一度見積りを取ってください")
}
