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
		// Seedance 2.5 prices in tokens ($10.70 per million at 480p and 720p, $11.70 at 1080p; no video input): the official
		// per-second figures of a 16:9 clip (ModelArk pricing page, billing examples).
		domain.ModelSeedance25: {Model: "dreamina-seedance-2-5-260628", CostPerSecond: map[string]float64{"480p": 0.103, "720p": 0.231, "1080p": 0.569}},
		// MiniMax H3 / H3 Max price by the second of output video (Pay-as-you-go pricing page): H3 USD 0.08 at 768P and 0.13 at 2K,
		// H3 Max 0.05 at 480P and 0.08 at 768P. Input images beyond the free quota are billed apart, and mam never sends more
		// images than the quota (MaxReferenceImages in Capabilities), so the per-second price is the whole cost.
		domain.ModelMiniMaxH3:    {Model: "MiniMax-H3", CostPerSecond: map[string]float64{"768p": 0.08, "2k": 0.13}},
		domain.ModelMiniMaxH3Max: {Model: "MiniMax-H3-Max", CostPerSecond: map[string]float64{"480p": 0.05, "768p": 0.08}},
		// Wan 3.0 prices by the second of output video, in the Singapore region (the model page's table; the other regions are
		// 0.041256 / 0.082513 / 0.165025 USD at 480P / 720P / 1080P: set cost_per_second_usd when the base URL is of another region).
		domain.ModelWan3: {Model: "wan3.0-video", CostPerSecond: map[string]float64{"480p": 0.05, "720p": 0.10, "1080p": 0.20}},
	}
}
