package gentest

import (
	"context"
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/prompttranslate"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/failtext"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// DefaultTranslate is whether a batch translates its prompts into English when the caller does not say (the screen's switch starts
// in this state, and the MCP tools take it for an omitted translate_to_english).
const DefaultTranslate = true

// TranslateLine is the Model of the line of the estimate that is the translation (the line has no model of generation).
const TranslateLine = "translate"

// translated is what a batch sends in place of the prompts that were translated, by the prompt as it was typed. A prompt that
// is not in it (English already) is sent as it is.
type translated map[PromptInput]PromptInput

// sendOf is the prompt that is sent for p.
func (t translated) sendOf(p PromptInput) PromptInput {
	if en, ok := t[p]; ok {
		return en
	}
	return p
}

// textsToTranslate are the distinct texts of the request (prompts and negative prompts) that are not English.
func textsToTranslate(r Request) []string {
	if !r.TranslateToEnglish {
		return nil
	}
	var all []string
	for _, p := range r.Prompts {
		all = append(all, p.Prompt, p.Negative)
	}
	return prompttranslate.Select(all)
}

// addTranslation puts the translation into the estimate (nothing when no text is to be translated): a line of the breakdown, which is
// not a result (it is not counted in UnknownItems). The call of the Reasoning LLM is priced per token at the price of the target of
// its route (llm.PriceOf; the tokens are an assumption on the safe side, prompttranslate.UsageOf), so the line is in the total
// (Items is the texts, UnitUSD the share of one of them in the one call). A target without a checked price (a ChatGPT plan, a model
// that is not in the table) is left out of the total as the line of a model without a price is: named in UnknownModels with the
// warning no_price (#1069).
func (s *Service) addTranslation(est *Estimate, r Request) error {
	texts := textsToTranslate(r)
	if len(texts) == 0 {
		return nil
	}
	if s.Translator == nil {
		return gterr.Invalid("英訳に使う LLM が使えません。英訳を OFF にしてください")
	}
	t, call := s.Translator.Plan(texts)
	label := fmt.Sprintf("プロンプトの英訳（%s / %s）", t.Provider, t.Model)
	line := ModelEstimate{Model: TranslateLine, Label: label, Items: len(texts), Basis: modelcatalog.BasisUnknown}
	if call == nil {
		est.UnknownModels = append(est.UnknownModels, TranslateLine)
		est.Warnings = append(est.Warnings, Warning{Model: TranslateLine, Code: WarnNoPrice, Message: label + "は LLM の単価が分からないため、見積りの合計に入れません"})
	} else {
		unit, sub := *call/float64(len(texts)), round4(*call)
		line.Basis, line.UnitUSD, line.SubtotalUSD = modelcatalog.BasisPriced, &unit, &sub
		if *call == 0 { // a model of the local server runs on the user's own PC
			line.Basis = modelcatalog.BasisFree
		}
		est.TotalUSD += sub
	}
	est.Breakdown = append(est.Breakdown, line)
	return nil
}

// translate translates the prompts of the request once, for every model and every result of the batch. Nothing of the batch
// exists yet: a failure of the LLM is a *gterr.UpstreamError, and a prompt is never sent in the language it was typed in because the
// translation failed. A request that was cancelled (or timed out) while the LLM was asked is no failure of the LLM: the error of the
// context is returned as it is (errors.Is finds context.Canceled), not as an upstream refusal, and it is not logged as a failed
// translation (#1072). A text the LLM returned unchanged (50Ω resistor) is no failure: its prompt is sent as typed and is not
// in the returned map.
func (s *Service) translate(ctx context.Context, r Request) (translated, error) {
	texts := textsToTranslate(r)
	if len(texts) == 0 {
		return nil, nil
	}
	if s.Translator == nil {
		return nil, gterr.Invalid("英訳に使う LLM が使えません。英訳を OFF にしてください")
	}
	english, err := s.Translator.Translate(ctx, texts)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		s.Logger.Warn("translate generation test prompts failed", "err", err)
		return nil, &gterr.UpstreamError{Message: failtext.Translate(err), Err: err}
	}
	en := make(map[string]string, len(texts))
	for i, t := range texts {
		en[t] = english[i]
	}
	out := translated{}
	for _, p := range r.Prompts {
		to := PromptInput{Prompt: orSame(en, p.Prompt), Negative: orSame(en, p.Negative)}
		if to != p {
			out[p] = to
			if lost := lostWeights(p, to); len(lost) > 0 {
				s.Logger.Warn("the translation of a generation test prompt lost prompt weights", "lost", lost)
			}
		}
	}
	return out, nil
}

func orSame(m map[string]string, s string) string {
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

// record is what a result of the prompt p keeps of its translation (nil when p was sent as typed).
func (t translated) record(p PromptInput) *record.SentText {
	en, ok := t[p]
	if !ok {
		return nil
	}
	return &record.SentText{Prompt: en.Prompt, Negative: en.Negative, LostWeights: lostWeights(p, en)}
}

// lostWeights lists the weights of the prompt and the negative prompt typed as p that the English en does not have, without repeating
// one that both lost.
func lostWeights(p, en PromptInput) []string {
	var out []string
	for _, w := range append(prompttranslate.LostWeights(p.Prompt, en.Prompt), prompttranslate.LostWeights(p.Negative, en.Negative)...) {
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}
