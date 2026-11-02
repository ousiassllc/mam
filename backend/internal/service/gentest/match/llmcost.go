package match

import (
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// addLLM puts the LLM work of at most `rounds` rounds into the estimate (#965): a line for the work that writes and fixes the prompts
// and a line for the work that compares the pictures, each priced per token at the model of its route (llm.PriceOf). A line whose
// model has no checked price (the ChatGPT plans, a model that is not in the table) is left out of the total with the warning
// no_price, as the line of a model of generation without a price is: a price that has not been checked is never made up. The tokens
// of a call are an assumption on the safe side (imagematch.Matcher.Plan), so these lines are a guide, not a bill.
func (s *Service) addLLM(e *Estimate, rounds int) {
	write, compare := s.m.Plan(rounds)
	for _, line := range []struct {
		model, what string
		work        imagematch.Work
	}{
		{LLMLine, "プロンプトの作成・修正", write},
		{CompareLine, "似てる度の比較", compare},
	} {
		label := fmt.Sprintf("%s（%s / %s）", line.what, line.work.Target.Provider, line.work.Target.Model)
		l := gentest.ModelEstimate{Model: line.model, Label: label, Items: line.work.Calls, Basis: modelcatalog.BasisUnknown, UnitUSD: line.work.UnitUSD}
		if line.work.UnitUSD == nil {
			e.Warnings = append(e.Warnings, gentest.Warning{Model: line.model, Code: gentest.WarnNoPrice, Message: label + " は単価が分からないため、見積りの合計に入れません"})
		} else {
			sub := round4(*line.work.UnitUSD * float64(line.work.Calls))
			l.Basis, l.SubtotalUSD = modelcatalog.BasisPriced, &sub
			e.TotalUSD += sub
		}
		e.Breakdown = append(e.Breakdown, l)
	}
}
