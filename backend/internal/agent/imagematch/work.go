package imagematch

import "github.com/ousiassllc/mam/backend/internal/agent/llm"

// The tokens a call is expected to use, for the estimate. They are an assumption on the safe side, not a measurement (the answer of a
// call does not say how many tokens it used): the picture is the 1024 px picture of Prepare, and the answer has room for the prompt
// of MaxPromptRunes characters and for the reasoning of a model that thinks first. The price per token is the provider's.
const (
	// pictureTokens is a picture of up to 1024 px on its long side: Anthropic's rule (width x height / 750) gives about 1400 tokens
	// for a square one, the others are in the same range.
	pictureTokens = 1600
)

var (
	// describeUsage: the system prompt and the note, one picture; the prompt.
	describeUsage = llm.Usage{InputTokens: pictureTokens + 1000, OutputTokens: 2000}
	// compareUsage: the system prompt, two pictures; the score and at most MaxDifferences sentences.
	compareUsage = llm.Usage{InputTokens: 2*pictureTokens + 500, OutputTokens: 1000}
	// reviseUsage: the system prompt, the prompt that is fixed, its differences and the words, two pictures; the whole fixed prompt.
	reviseUsage = llm.Usage{InputTokens: 2*pictureTokens + 2500, OutputTokens: 2500}
)

// Work is the LLM work of one kind in a run, for the estimate: which target does it, how many calls at most, and what a call is
// estimated to cost (nil when the app has no price for the target: the ChatGPT plans, and any model that has not been checked).
type Work struct {
	Target  llm.Target
	Calls   int
	UnitUSD *float64
}

// Plan is the LLM work of a run of at most `rounds` rounds. Every round writes (the first of a new match describes the reference, the
// others fix a prompt) and then compares, so each kind is called once a round. A call that writes is estimated as the dearer of the
// two (a fix looks at two pictures and the whole prompt), so the estimate is never lower than what the run can cost. The calls follow
// the routes with their fail-over: the estimate prices the primary target.
func (m *Matcher) Plan(rounds int) (write, compare Work) {
	return Work{m.create, rounds, unit(m.create, reviseUsage, describeUsage)}, Work{m.judge, rounds, unit(m.judge, compareUsage)}
}

// unit is the dearest of the usages at the price of the target.
func unit(t llm.Target, usages ...llm.Usage) *float64 {
	price, ok := llm.PriceOf(t)
	if !ok {
		return nil
	}
	var most float64
	for _, u := range usages {
		most = max(most, price.Cost(u))
	}
	return &most
}
