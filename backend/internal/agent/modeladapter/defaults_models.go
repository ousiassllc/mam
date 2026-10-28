package modeladapter

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// defaultModels are the settings of the built-in models (spec/architecture/model-adapters.md §9.2, §9.4). The prices are
// reference values: they feed the estimated cost, not billing.
func defaultModels() map[domain.ModelName]ModelConfig {
	return map[domain.ModelName]ModelConfig{
		// Prices: Google's Gemini API pricing page (1K image, the size an unset image_size gives), BFL's
		// pricing page (FLUX.2 [pro], text only, up to 1 megapixel). GPT Image 2 is priced by the quality
		// profile it is sent and the size (package gptimage; spec/architecture/model-adapters.md §9.2).
		domain.ModelNanoBanana:     {Model: "gemini-nano-banana-2.1", CostPerImage: 0.0336},
		domain.ModelNanoBananaLite: {Model: "gemini-3.1-flash-lite-image", CostPerImage: 0.0336},
		domain.ModelFlux2:          {Model: "flux-2-pro", CostPerImage: 0.03},
		domain.ModelGPTImage2:      {Model: gptimage.APIModel, Quality: string(gptimage.Default)},
		chatgptid.Model:            {Model: chatgptid.APIModel}, // the plan pays: 0 USD per image
		domain.ModelKling3:         {Model: "kling-v3", CostPerSecond: map[string]float64{"720p": 0.07, "1080p": 0.14}},
		domain.ModelSeedance2:      {Model: "seedance-2-0", CostPerSecond: map[string]float64{"480p": 0.03, "720p": 0.08, "1080p": 0.18}},
		// Runway prices in credits (12 credits per second of Gen-4.5, 1 credit = USD 0.01).
		domain.ModelRunwayGen45: {Model: "gen4.5", CostPerSecond: map[string]float64{"720p": 0.12}},
		// Gemini Omni Flash prices in tokens ($17.50 per million video output tokens, 5,792 tokens per second of 720p
		// video): about USD 0.10 per second, the official figure.
		domain.ModelGeminiOmniFlash: {Model: "gemini-omni-1.1-flash", CostPerSecond: map[string]float64{"720p": 0.10}},
		// Kling 4.0 prices in units (1 unit = USD 0.14 list price): 1.0 unit per second at 720p, 1.5 at 1080p, with no video
		// input. The pricing page marks them "preview": Kling 4.0 is in early access (spec/architecture/model-adapters.md §9.4).
		domain.ModelKling4: {Model: "kling-4.0", CostPerSecond: map[string]float64{"720p": 0.14, "1080p": 0.21}},
	}
}
