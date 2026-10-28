package gentest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"

	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// ModelEstimate is the line of one model in an estimate. UnitUSD and SubtotalUSD are nil when the basis is unknown: the
// model's results are counted but not priced.
type ModelEstimate struct {
	Model string `json:"model"`
	Label string `json:"label"`
	Items int    `json:"items"`
	Basis string `json:"basis" jsonschema:"priced / free（ローカル: 0 USD）/ unknown（単価なし。合計に入れない）"`
	// UnitUSD is the estimate of one result: the price of an image, or the per-second price at the resolution times the
	// length the model makes (video).
	UnitUSD     *float64 `json:"unit_usd,omitempty"`
	SubtotalUSD *float64 `json:"subtotal_usd,omitempty"`
	Resolution  string   `json:"resolution,omitempty"`
	Seconds     float64  `json:"seconds,omitempty"`
}

// Estimate is what the confirmation shows: how many results the batch makes, what they are estimated to cost per model and in
// total, and the warnings. The estimate of a model without a price is left out of TotalUSD (UnknownModels says which).
type Estimate struct {
	// ID identifies the request and the estimate: Run refuses a confirmation whose ID is not the one of the request it
	// is asked to run now (the prices or the models changed, or the request is another one).
	ID            string          `json:"estimate_id"`
	Request       Request         `json:"request"`
	TotalItems    int             `json:"total_items"`
	Breakdown     []ModelEstimate `json:"breakdown"`
	TotalUSD      float64         `json:"total_usd"`
	UnknownItems  int             `json:"unknown_items"`
	UnknownModels []string        `json:"unknown_models"`
	Warnings      []Warning       `json:"warnings"`
	MaxItems      int             `json:"max_items"`
	MaxCostUSD    float64         `json:"max_cost_usd" jsonschema:"1 回のバッチの見積り額の上限（0 は上限なし）"`
	// OverLimit is why the batch cannot run ("" when it can): the estimate is over max_cost_usd.
	OverLimit string `json:"over_limit,omitempty"`
}

// Estimate prices the request without running anything. A request that cannot run at all (no model, no prompt, a model that
// is not available, more results than the limit) is refused with *gterr.InvalidError (so is an input image that cannot be read); one that is over the cost limit is returned
// with OverLimit set, so the screen can show the figures.
func (s *Service) Estimate(ctx context.Context, r Request) (Estimate, error) {
	est, _, err := s.estimate(ctx, r)
	if err != nil {
		return Estimate{}, err
	}
	for _, ref := range est.Request.References { // an input that is gone is refused now, not after the confirmation
		if _, err := s.resolve(ctx, ref); err != nil {
			return Estimate{}, err
		}
	}
	return est, nil
}

func (s *Service) estimate(_ context.Context, r Request) (Estimate, []modelPlan, error) {
	r, models, err := s.normalize(r)
	if err != nil {
		return Estimate{}, nil, err
	}
	cfg := s.Adapter.Config()
	perModel := len(r.Prompts) * r.Count
	est := Estimate{
		Request: r, TotalItems: len(models) * perModel, Breakdown: []ModelEstimate{}, UnknownModels: []string{}, Warnings: []Warning{},
		MaxItems: cfg.Gentest.Items(), MaxCostUSD: cfg.Gentest.CostUSD(),
	}
	plans := make([]modelPlan, 0, len(models))
	for _, m := range models {
		p := s.planModel(cfg, r, m)
		plans = append(plans, p)
		line := ModelEstimate{Model: m.ID, Label: m.Label, Items: perModel, Basis: p.basis, UnitUSD: p.unit}
		if r.Media == gentestrepo.MediaVideo {
			line.Resolution, line.Seconds = p.resolution, p.seconds
		}
		if p.unit != nil {
			sub := round4(*p.unit * float64(perModel))
			line.SubtotalUSD = &sub
			est.TotalUSD += sub
		} else {
			est.UnknownItems += perModel
			est.UnknownModels = append(est.UnknownModels, m.ID)
		}
		est.Breakdown = append(est.Breakdown, line)
		est.Warnings = append(est.Warnings, p.warnings...)
	}
	est.TotalUSD = round4(est.TotalUSD)
	if est.MaxCostUSD > 0 && est.TotalUSD > est.MaxCostUSD {
		est.OverLimit = fmt.Sprintf("見積り %s USD が 1 回の上限 %s USD を超えています。モデル・プロンプト・本数を減らしてください（上限は models.json の gentest.max_cost_usd）",
			usd(est.TotalUSD), usd(est.MaxCostUSD))
	}
	est.ID = estimateID(est)
	return est, plans, nil
}

func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

// usd writes an amount the way the screens do.
func usd(v float64) string { return fmt.Sprintf("%.4f", v) }

// lineBreaks writes every line break (CRLF, CR) as LF for the digest of an estimate.
var lineBreaks = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// estimateID is a digest of the normalized request and of the figures the user confirmed. The line breaks of the prompts are
// not part of what is compared: a browser posts the text of a textarea with CRLF, but reads the confirmation's hidden field back
// with LF, so the same prompt would otherwise never match the run that follows its own estimate. The prompts themselves are
// sent as they are.
func estimateID(e Estimate) string {
	r := e.Request
	r.Prompts = slices.Clone(r.Prompts)
	for i, p := range r.Prompts {
		r.Prompts[i] = PromptInput{Prompt: lineBreaks.Replace(p.Prompt), Negative: lineBreaks.Replace(p.Negative)}
	}
	raw, _ := json.Marshal(struct {
		Request Request         `json:"r"`
		Lines   []ModelEstimate `json:"l"`
		Total   string          `json:"t"`
	}{r, e.Breakdown, usd(e.TotalUSD)})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

// Confirmation is what a caller hands Run: the ID of the estimate it showed and the total in USD it showed.
type Confirmation struct {
	EstimateID string  `json:"estimate_id"`
	TotalUSD   float64 `json:"confirmed_total_usd"`
}

// check refuses a run whose request or figures are not the ones that were confirmed, or that is over the cost limit.
func (c Confirmation) check(e Estimate) error {
	switch {
	case c.EstimateID == "":
		return gterr.Invalid("見積りを確認してから実行してください（estimate_id がありません）")
	case c.EstimateID != e.ID, usd(c.TotalUSD) != usd(e.TotalUSD):
		return gterr.Conflict("確認した見積りと今の見積りが違います（単価・モデルの状態・条件が変わりました）。見積りをやり直して確認してください（今の合計 %s USD、%d 本）", usd(e.TotalUSD), e.TotalItems)
	case e.OverLimit != "":
		return gterr.Conflict("%s", e.OverLimit)
	}
	return nil
}
