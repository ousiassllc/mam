package prompttranslate

import (
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

// The tokens of a Translate call, for the estimate (#1069). They are an assumption on the safe side, not a measurement, as the
// tokens of the LLM work of a match are (imagematch): the provider's price per token (llm.PriceOf) is applied to them.
const (
	// tokensPerRune is what a character of a prompt is counted as, on the way in (a Japanese character is one token or two) and on the
	// way out (the English of it is fewer tokens than the characters, but the answer is a JSON object around it).
	tokensPerRune = 2
	// reasoningTokens is the room of a model that thinks before it answers.
	reasoningTokens = 1000
	// bytesPerSystemToken counts the tokens of the English instruction (about 4 bytes a token; 3 keeps the figure on the high side).
	bytesPerSystemToken = 3
)

// UsageOf is the tokens one Translate call of texts is estimated to use: the instruction and the texts in, the English of the texts
// (and the room of a model that reasons) out.
func UsageOf(texts []string) llm.Usage {
	var runes int
	for _, s := range texts {
		runes += utf8.RuneCountInString(s)
	}
	return llm.Usage{
		InputTokens:  len(system)/bytesPerSystemToken + runes*tokensPerRune,
		OutputTokens: reasoningTokens + runes*tokensPerRune,
	}
}

// Plan is what the estimate says of translating texts in one call: the target of the primary of the route (as Target says at the time of
// the call) and the price of the call at that target's price per token. The price is nil when the app has no checked price for the
// target (a ChatGPT plan, a model that is not in the table): such a call is never given a price that was made up.
func (t *Translator) Plan(texts []string) (target llm.Target, usd *float64) {
	target = t.Target()
	price, ok := llm.PriceOf(target)
	if !ok {
		return target, nil
	}
	cost := price.Cost(UsageOf(texts))
	return target, &cost
}
