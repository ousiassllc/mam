package llm

// Usage is the tokens of one call, or the tokens a call is expected to use.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Price is what a provider charges for a model per million tokens, in USD (the standard tier, a prompt of an ordinary length).
type Price struct {
	InputPerMTokUSD  float64
	OutputPerMTokUSD float64
}

// Cost is the price of a call that uses u.
func (p Price) Cost(u Usage) float64 {
	return (float64(u.InputTokens)*p.InputPerMTokUSD + float64(u.OutputTokens)*p.OutputPerMTokUSD) / 1e6
}

// prices are the published per-token prices of the models that were checked against the provider's own price page on 2026-11-02:
//
//	Anthropic: https://platform.claude.com/docs/en/about-claude/pricing
//	OpenAI:    https://developers.openai.com/api/docs/pricing
//	Google:    https://ai.google.dev/gemini-api/docs/pricing
//
// A model that is not here has no price in the app (PriceOf says so, and an estimate leaves it out of the total with a warning): a
// figure that has not been checked is never made up. Add a model only with its row of the provider's page. The ChatGPT plans
// (ProviderChatGPTSubscription, ProviderChatGPTCodex) are not here on purpose: their use is not billed per token.
// Names are the model ids the API takes, exactly: a dated snapshot or a preview alias is another id.
var prices = map[Target]Price{
	{ProviderAnthropic, "claude-opus-4-5"}:   {5, 25},
	{ProviderAnthropic, "claude-sonnet-4-5"}: {3, 15},
	{ProviderAnthropic, "claude-haiku-4-5"}:  {1, 5},

	{ProviderOpenAI, "gpt-5"}:      {1.25, 10},
	{ProviderOpenAI, "gpt-5-mini"}: {0.25, 2},
	{ProviderOpenAI, "gpt-5-nano"}: {0.05, 0.40},
	{ProviderOpenAI, "gpt-5.1"}:    {1.25, 10},
	{ProviderOpenAI, "gpt-5.2"}:    {1.75, 14},

	// Gemini 3.1 Pro Preview: the price of a prompt of up to 200k tokens (the longer ones cost more; the calls of the app are far shorter).
	{ProviderGoogle, "gemini-3.1-pro-preview"}: {2, 12},
	{ProviderGoogle, "gemini-3.5-flash-lite"}:  {0.30, 2.50},
}

// PriceOf is the price of a target; false when the app has no checked price for it.
func PriceOf(t Target) (Price, bool) {
	p, ok := prices[t]
	return p, ok
}
