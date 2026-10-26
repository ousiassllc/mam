// Package gptimage is what the Model Adapter knows about the price of GPT Image 2: the quality profile mam sends,
// the price of one image by quality and size (the estimate) and the cost of a call computed from the token counts
// of its response (the actual cost). The figures are those of OpenAI's published pricing, with their source and the
// day they were read in spec/architecture/model-adapters.md §9.2. It is a package of its own because package
// modeladapter is over its line limit.
package gptimage

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/aspectsize"
)

// APIModel is the API model ID the price table below belongs to.
const APIModel = "gpt-image-2"

// Quality is the profile of the `quality` request field (the setting of models.gpt-image-2.quality). It is always sent
// explicitly: `auto` lets the model choose, and the price of an image depends on the choice.
type Quality string

// The qualities gpt-image-2 takes besides `auto` (which mam does not use).
const (
	Low    Quality = "low"
	Medium Quality = "medium"
	High   Quality = "high"
)

// Default is the profile of a setting that names none.
const Default = Medium

// Qualities lists every profile.
func Qualities() []Quality { return []Quality{Low, Medium, High} }

// Effective is the profile a setting stands for: Default when it names none.
func Effective(setting string) Quality {
	if setting == "" {
		return Default
	}
	return Quality(setting)
}

// Valid reports whether the setting is empty (Default) or one of Qualities.
func Valid(setting string) bool {
	return setting == "" || slices.Contains(Qualities(), Quality(setting))
}

// Priced reports whether the price table applies to the API model: gpt-image-2 and its dated snapshots. Another
// model ID (a setting of the user) has other prices and gets none here.
func Priced(apiModel string) bool {
	return apiModel == APIModel || strings.HasPrefix(apiModel, APIModel+"-")
}

// perImage is the price (USD) of one image by quality and size: the "GPT Image 2" rows of the table in OpenAI's image
// generation guide (https://developers.openai.com/api/docs/guides/image-generation, "Calculating costs").
var perImage = map[Quality]map[string]float64{
	Low:    {"1024x1024": 0.006, "1024x1536": 0.005, "1536x1024": 0.005},
	Medium: {"1024x1024": 0.053, "1024x1536": 0.041, "1536x1024": 0.041},
	High:   {"1024x1024": 0.211, "1024x1536": 0.165, "1536x1024": 0.165},
}

// Estimate is the price of one image of the profile at the size mam asks for the aspect ratio (aspectsize.OpenAI);
// an empty aspect ratio gives the dearest size. It is 0 for a profile that is not one of Qualities.
func Estimate(setting, aspect string) float64 {
	prices := perImage[Effective(setting)]
	if aspect != "" {
		return prices[aspectsize.OpenAI(aspect)]
	}
	most := 0.0
	for _, p := range prices {
		most = max(most, p)
	}
	return most
}

// Prices of one million tokens (USD), from the "gpt-image-2" rows of OpenAI's pricing page (Standard).
const (
	textInputPerMillion  = 5.0
	imageInputPerMillion = 8.0
	outputPerMillion     = 30.0
)

// Usage is the `usage` of an Images API response (openai-openapi `ImagesResponse`).
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	InputDetails struct {
		TextTokens  int `json:"text_tokens"`
		ImageTokens int `json:"image_tokens"`
	} `json:"input_tokens_details"`
}

// ParseUsage decodes the `usage` member of a response. `usage` only refines the cost of an image that is already
// paid for, so one that is absent, null or malformed (a count that is a string or a fraction, or too large for an
// integer) gives nil, never an error: the caller still has the image and the estimate stands.
func ParseUsage(raw []byte) *Usage {
	var u *Usage
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil
	}
	return u
}

// The ceilings of a token count that can be real. mam's largest image (high quality, 1024×1024) is about 7,000
// output tokens ($0.211 at $30 per million); a few high-fidelity reference images are a few thousand input tokens
// each. The ceilings are far above that: a count over one comes from a corrupt or hostile response and, once it is
// a cost that is summed into the budget, would exhaust it.
const (
	maxOutputTokens = 100_000
	maxInputTokens  = 200_000
)

func within(n, ceiling int) bool { return n >= 0 && n <= ceiling }

// ActualCost is the cost (USD) of a call computed from its token counts: text and image input tokens and the output
// image tokens at the prices above. It is nil when the response has no usable usage (none, or a count that is
// negative or over its ceiling), or the model is not priced here; the estimate is used then. Input tokens the details
// do not account for count as text, the cheaper kind of input, so the cost is not overstated.
func ActualCost(apiModel string, u *Usage) *float64 {
	if u == nil || !Priced(apiModel) {
		return nil
	}
	text, image := u.InputDetails.TextTokens, u.InputDetails.ImageTokens
	if !within(u.InputTokens, maxInputTokens) || !within(text, maxInputTokens) || !within(image, maxInputTokens) || !within(u.OutputTokens, maxOutputTokens) {
		return nil
	}
	text += max(u.InputTokens-text-image, 0)
	cost := (float64(text)*textInputPerMillion + float64(image)*imageInputPerMillion + float64(u.OutputTokens)*outputPerMillion) / 1e6
	return &cost
}
