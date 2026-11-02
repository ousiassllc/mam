package match

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// Request is a prompt match to estimate and run: a new one (Reference and Model) or the go-on of one that is over (ContinueBatchID).
type Request struct {
	// Mode is auto (default), once or adjust.
	Mode string `json:"mode,omitempty"`
	// Reference is the one reference image of a new match. Model is the image model that generates.
	Reference gentest.RefInput `json:"reference,omitzero"`
	Model     string           `json:"model,omitempty"`
	// ContinueBatchID goes on with a match that is over: its reference image, model and aspect ratio stay.
	ContinueBatchID string `json:"continue_batch_id,omitempty"`
	// TargetScore is the similarity (1..100) at which an auto run stops (default 80; a continued match keeps its own).
	TargetScore int `json:"target_score,omitempty"`
	// MaxRounds is the most rounds of an auto run (1..10, default 5); once and adjust make one.
	MaxRounds int `json:"max_rounds,omitempty"`
	// MaxCostUSD is the most the user is willing to spend on this run (0: only the cap of models.json, gentest.max_cost_usd, applies):
	// an estimate over it does not start.
	MaxCostUSD  float64 `json:"max_cost_usd,omitempty"`
	AspectRatio string  `json:"aspect_ratio,omitempty"`
	// Instruction is what the user wants in words, in any language: it goes into the prompt written for the reference (a new match)
	// or into the first fix (a continued one).
	Instruction string `json:"instruction,omitempty"`
}

var aspectPattern = regexp.MustCompile(`^[1-9][0-9]?:[1-9][0-9]?$`)

// plan is a normalized request: what it runs on and how many rounds.
type plan struct {
	req    Request
	model  modelcatalog.Info
	rounds int
	// first is the number of rounds the match has already (a continued one goes on counting).
	first int
}

func cleanInstruction(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxInstructionRunes {
		return "", gterr.Invalid("指示は %d 文字以内にしてください", MaxInstructionRunes)
	}
	return s, nil
}

// normalize checks the request and fills the defaults. A continued match takes its model, aspect ratio and (when none is given)
// its target from the match itself.
func (s *Service) normalize(ctx context.Context, r Request) (plan, error) {
	if s.m == nil {
		return plan{}, gterr.Invalid("プロンプトを作る LLM が使えません。設定の「LLM の割り当て」を確認してください")
	}
	var p plan
	var err error
	if r.Mode == "" {
		r.Mode = ModeAuto
	}
	if r.Mode != ModeAuto && r.Mode != ModeOnce && r.Mode != ModeAdjust {
		return p, gterr.Invalid("進め方は auto / once / adjust のどれかにしてください")
	}
	if r.Instruction, err = cleanInstruction(r.Instruction); err != nil {
		return p, err
	}
	if r.ContinueBatchID != "" {
		if r, p.first, err = s.continued(ctx, r); err != nil {
			return p, err
		}
	} else if err = s.checkNew(r); err != nil {
		return p, err
	}
	if r.TargetScore == 0 {
		r.TargetScore = DefaultTarget
	}
	if r.TargetScore < 1 || r.TargetScore > 100 {
		return p, gterr.Invalid("目標の似てる度は 1〜100 にしてください")
	}
	if r.MaxRounds == 0 {
		r.MaxRounds = DefaultRounds
	}
	if r.MaxRounds < 1 || r.MaxRounds > MaxRounds {
		return p, gterr.Invalid("上限の回数は 1〜%d にしてください", MaxRounds)
	}
	if r.MaxCostUSD < 0 || math.IsNaN(r.MaxCostUSD) || math.IsInf(r.MaxCostUSD, 0) {
		return p, gterr.Invalid("上限の費用は 0 以上の USD で指定してください")
	}
	p.rounds = 1
	if r.Mode == ModeAuto {
		p.rounds = r.MaxRounds
	} else {
		r.MaxRounds = 1
	}
	if items, _ := s.g.Limits(); p.rounds > items {
		return p, gterr.Invalid("1 回で作れるのは %d 本までです", items)
	}
	if p.model, err = s.g.Model(ctx, gentestrepo.MediaImage, r.Model); err != nil {
		return p, gterr.Invalid("モデル %q は選べません（画像モデルの一覧にありません）", r.Model)
	}
	if !p.model.Available {
		return p, gterr.Invalid("モデル %s は今使えません: %s", p.model.Label, p.model.UnavailableMessage)
	}
	p.req = r
	return p, nil
}

func (s *Service) checkNew(r Request) error {
	if r.Reference.ID == "" {
		return gterr.Invalid("参考にする画像を 1 枚選んでください")
	}
	if r.Reference.Role != "" && r.Reference.Role != gentest.RoleReference {
		return gterr.Invalid("参考画像の役割は指定できません")
	}
	if r.Model == "" {
		return gterr.Invalid("画像を作るモデルを選んでください")
	}
	if r.AspectRatio == "" {
		return nil
	}
	if !aspectPattern.MatchString(r.AspectRatio) {
		return gterr.Invalid("縦横比は 16:9 のように指定してください")
	}
	return nil
}

// continued fills the request of a go-on from the match it goes on with, which must be over.
func (s *Service) continued(ctx context.Context, r Request) (Request, int, error) {
	if r.Reference.ID != "" || r.Model != "" || r.AspectRatio != "" {
		return r, 0, gterr.Invalid("続きでは、参考画像・モデル・縦横比は最初のマッチのものを使います")
	}
	b, err := s.g.Repo.GetBatch(ctx, r.ContinueBatchID)
	if err != nil {
		return r, 0, gterr.NotFound(err)
	}
	bp, ok := decodeBatch(b)
	if !ok {
		return r, 0, gterr.Invalid("このバッチはプロンプトマッチではありません")
	}
	if b.FinishedAt == nil {
		return r, 0, gterr.Conflict("このマッチはまだ終わっていません。終わってから続けてください（途中の指示は「指示を足す」で入れられます）")
	}
	results, err := s.g.Repo.Results(ctx, b.ID)
	if err != nil {
		return r, 0, err
	}
	if len(results) == 0 {
		return r, 0, gterr.Conflict("このマッチには続ける元の結果がありません")
	}
	r.Model, r.AspectRatio = results[0].Model, bp.AspectRatio
	if r.TargetScore == 0 {
		r.TargetScore = bp.Match.Target
	}
	return r, len(results), nil
}

// Estimate is what the confirmation shows: how many rounds at most, what they are estimated to cost and the warnings. The work of
// the LLM has no price (a call of the Reasoning LLM is not in the Cost Ledger, and the plans of ChatGPT are not per call), so it
// is a line without one that is left out of the total, like the translation of a batch.
type Estimate struct {
	// ID identifies the request and the estimate: Run refuses a confirmation whose ID is not the one of the request it is asked to run now.
	ID         string                  `json:"estimate_id"`
	Request    Request                 `json:"request"`
	Rounds     int                     `json:"rounds" jsonschema:"今回の実行で生成する最大の回数（目標に届けばそこで止まる）"`
	Breakdown  []gentest.ModelEstimate `json:"breakdown"`
	TotalUSD   float64                 `json:"total_usd" jsonschema:"最大の回数ぶんの見積り（LLM の分は含まない）"`
	Warnings   []gentest.Warning       `json:"warnings"`
	MaxCostUSD float64                 `json:"max_cost_usd" jsonschema:"1 回の見積り額の上限（0 は上限なし）"`
	// OverLimit is why the match cannot run ("" when it can): the estimate is over max_cost_usd.
	OverLimit string `json:"over_limit,omitempty"`
}

// Estimate prices the request without running anything. A request that cannot run is a *gterr.InvalidError (so is a reference image
// that cannot be read); one that is over the cost limit comes back with OverLimit set, so that the screen can show the figures.
func (s *Service) Estimate(ctx context.Context, r Request) (Estimate, error) {
	e, _, err := s.estimate(ctx, r)
	return e, err
}

func (s *Service) estimate(ctx context.Context, r Request) (Estimate, plan, error) {
	p, err := s.normalize(ctx, r)
	if err != nil {
		return Estimate{}, p, err
	}
	if p.req.ContinueBatchID == "" {
		if _, err := s.g.ResolveInput(ctx, p.req.Reference); err != nil {
			return Estimate{}, p, err
		}
	}
	unit, basis := s.unitPrice(p)
	cfg := s.g.Adapter.Config()
	e := Estimate{Request: p.req, Rounds: p.rounds, Breakdown: []gentest.ModelEstimate{}, Warnings: []gentest.Warning{}, MaxCostUSD: cfg.Gentest.CostUSD()}
	line := gentest.ModelEstimate{Model: p.model.ID, Label: p.model.Label, Items: p.rounds, Basis: basis, UnitUSD: unit}
	if unit != nil {
		sub := round4(*unit * float64(p.rounds))
		line.SubtotalUSD, e.TotalUSD = &sub, sub
	} else {
		e.Warnings = append(e.Warnings, gentest.Warning{Model: p.model.ID, Code: gentest.WarnNoPrice, Message: p.model.Label + " は単価が分からないため、見積りの合計に入れません"})
	}
	e.Breakdown = append(e.Breakdown, line)
	create, judge := s.m.Targets()
	label := fmt.Sprintf("プロンプトの作成・修正（%s / %s）・比較（%s / %s）", create.Provider, create.Model, judge.Provider, judge.Model)
	if create == judge {
		label = fmt.Sprintf("プロンプトの作成・比較・修正（%s / %s）", create.Provider, create.Model)
	}
	e.Breakdown = append(e.Breakdown, gentest.ModelEstimate{Model: LLMLine, Label: label, Items: 2*p.rounds + 1, Basis: modelcatalog.BasisUnknown})
	e.Warnings = append(e.Warnings, gentest.Warning{Model: LLMLine, Code: gentest.WarnNoPrice, Message: "LLM の呼び出しは単価が分からないため、見積りの合計に入れません"})
	switch {
	case p.req.MaxCostUSD > 0 && e.TotalUSD > p.req.MaxCostUSD:
		e.OverLimit = fmt.Sprintf("見積り %.4f USD が指定した上限の費用 %.4f USD を超えています。上限の回数を減らすか、安いモデルにしてください", e.TotalUSD, p.req.MaxCostUSD)
	case e.MaxCostUSD > 0 && e.TotalUSD > e.MaxCostUSD:
		e.OverLimit = fmt.Sprintf("見積り %.4f USD が 1 回の上限 %.4f USD を超えています。上限の回数を減らすか、安いモデルにしてください（上限は models.json の gentest.max_cost_usd）", e.TotalUSD, e.MaxCostUSD)
	}
	e.ID = estimateID(e)
	return e, p, nil
}

// unitPrice is the price of one picture of the model, nil when it is not known (the price of an image can depend on its size: the
// catalog holds the dearest one, and the setting knows the size).
func (s *Service) unitPrice(p plan) (*float64, string) {
	zero := 0.0
	switch p.model.Price.Basis {
	case modelcatalog.BasisFree:
		return &zero, modelcatalog.BasisFree
	case modelcatalog.BasisPriced:
		if p.model.Price.PerImageUSD != nil {
			unit := s.g.Adapter.Config().ImageCostFor(domain.ModelName(p.model.ID), aspectOf(p.req))
			return &unit, modelcatalog.BasisPriced
		}
	}
	return nil, modelcatalog.BasisUnknown
}

// aspectOf is the aspect ratio of the request (1:1 when none is given).
func aspectOf(r Request) string {
	if r.AspectRatio == "" {
		return "1:1"
	}
	return r.AspectRatio
}

func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

func estimateID(e Estimate) string {
	raw, _ := json.Marshal(struct {
		Request   Request                 `json:"r"`
		Rounds    int                     `json:"n"`
		Breakdown []gentest.ModelEstimate `json:"l"`
		Total     string                  `json:"t"`
	}{e.Request, e.Rounds, e.Breakdown, fmt.Sprintf("%.4f", e.TotalUSD)})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

// check refuses a run whose request or figures are not the ones that were confirmed, or that is over the cost limit.
func check(c gentest.Confirmation, e Estimate) error {
	switch {
	case c.EstimateID == "":
		return gterr.Invalid("見積りを確認してから実行してください（estimate_id がありません）")
	case c.EstimateID != e.ID, fmt.Sprintf("%.4f", c.TotalUSD) != fmt.Sprintf("%.4f", e.TotalUSD):
		return gterr.Conflict("確認した見積りと今の見積りが違います（単価・モデルの状態・条件が変わりました）。見積りをやり直して確認してください（今の合計 %.4f USD、最大 %d 回）", e.TotalUSD, e.Rounds)
	case e.OverLimit != "":
		return gterr.Conflict("%s", e.OverLimit)
	}
	return nil
}
