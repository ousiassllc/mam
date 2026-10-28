package credentials

import view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/credentials"

// info is what a provider's card says besides its name: what the key is for and where to get one.
type info struct {
	purpose string
	// keyURL is a page of the provider's own documentation or console where a key is created. Only the ones found in the
	// provider's official documentation (2026-10-25) are here; a provider without one has no link.
	keyURL string
}

// providerInfo is the purpose of each key (spec/architecture/model-adapters.md) by provider ID.
var providerInfo = map[string]info{
	"anthropic":  {"テキスト生成（Claude。Agent の LLM）", "https://platform.claude.com/settings/keys"},
	"openai":     {"テキスト生成（GPT-5 系）と画像生成（GPT Image 2）", ""},
	"google":     {"テキスト生成（Gemini）・画像生成（Nano Banana）・動画生成（Gemini Omni Flash）", "https://aistudio.google.com/apikey"},
	"flux":       {"画像生成（FLUX.2）", "https://dashboard.bfl.ai"},
	"kling":      {"動画生成（Kling 3.0。キーは ACCESS_KEY:SECRET_KEY の形。Kling 4.0 は次の「Kling 4.0」に API キーを入れる）", ""},
	"kling4":     {"動画生成（Kling 4.0。キーは Kling の API キー 1 つ。Kling 3.0 の ACCESS_KEY:SECRET_KEY とは別）", ""},
	"seedance":   {"動画生成（Seedance 2.0・2.5）", ""},
	"runway":     {"動画生成（Runway Gen-4.5）", "https://dev.runway.com"},
	"elevenlabs": {"音声（TTS・Music・SFX）", "https://elevenlabs.io/app/api/api-keys"},
	"synclabs":   {"Lip Sync", ""},
	"typesafe":   {"Repair の前段選別（Jev）", ""},
}

func (i info) apply(v *view.ProviderView) {
	v.Purpose, v.KeyURL = i.purpose, i.keyURL
}
