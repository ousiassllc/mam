package apikey

import (
	"slices"
	"strings"
	"unicode"
)

// provider is a service whose API key the settings screen manages (service.CredentialProviders: a test there keeps this table in step), with the names the director
// uses for it.
type provider struct {
	ID, Label string
	// Names are the lower-case words of the message that mean this provider (providerOf picks the longest match found, whatever the order here).
	Names []string
}

var providers = []provider{
	{"anthropic", "Anthropic（Claude）", []string{"anthropic", "claude", "クロード"}},
	{"openai", "OpenAI", []string{"openai", "オープンエーアイ", "gpt", "chatgpt"}},
	{"google", "Google（Gemini / Nano Banana）", []string{"google", "gemini", "nano banana", "グーグル", "ジェミニ"}},
	{"flux", "Black Forest Labs（Flux）", []string{"black forest labs", "black forest", "flux", "フラックス"}},
	{"kling", "Kling", []string{"kling", "クリング"}},
	{"kling4", "Kling 4.0", []string{"kling 4.0", "kling4", "kling 4", "クリング 4"}},
	{"seedance", "Seedance", []string{"seedance", "シードダンス"}},
	{"runway", "Runway", []string{"runway", "ランウェイ"}},
	{"minimax", "MiniMax（H3）", []string{"minimax", "ミニマックス"}},
	{"alibaba", "Alibaba Cloud（Wan 3.0）", []string{"alibaba", "アリババ", "wan 3.0", "wan"}},
	{"luma", "Luma（Ray 3.2）", []string{"luma", "ルマ"}},
	{"openrouter", "OpenRouter", []string{"openrouter", "open router", "オープンルーター"}},
	{"fal", "fal.ai", []string{"fal.ai", "fal"}},
	{"replicate", "Replicate", []string{"replicate", "レプリケート"}},
	{"topaz", "Topaz Labs（Topaz Video API）", []string{"topaz", "トパーズ"}},
	{"elevenlabs", "ElevenLabs", []string{"elevenlabs", "eleven labs", "イレブンラボ"}},
	{"synclabs", "Sync Labs", []string{"sync labs", "synclabs", "sync.so", "シンクラボ"}},
	{"typesafe", "TypeSafe（Jev）", []string{"typesafe", "jev", "タイプセーフ"}},
}

// providerOf finds the provider the text names: its ID, its label or one of its names. The longest name found wins ("Kling 4.0" is not "Kling"); false when none is named
// or when it names two that no longest match separates (the card then asks which).
func providerOf(text string) (provider, bool) { return providerAmong(text) }

// providerAmong is providerOf over the providers with these IDs (all of them when none is given).
func providerAmong(text string, ids ...string) (provider, bool) {
	lower := strings.ToLower(text)
	var (
		best    provider
		bestLen int
		tie     bool
	)
	for _, p := range providers {
		if len(ids) > 0 && !slices.Contains(ids, p.ID) {
			continue
		}
		n := longestMention(lower, p)
		switch {
		case n > bestLen:
			best, bestLen, tie = p, n, false
		case n > 0 && n == bestLen:
			tie = true
		}
	}
	return best, bestLen > 0 && !tie
}

func longestMention(lower string, p provider) int {
	longest := 0
	for _, name := range append([]string{p.ID}, p.Names...) {
		if len(name) > longest && mentions(lower, name) {
			longest = len(name)
		}
	}
	return longest
}

// mentions reports whether lower names `name` as a word of its own: an ASCII name inside another ASCII word ("wan" in "swan", "fal" in "false") is not a mention.
func mentions(lower, name string) bool {
	for from := 0; ; {
		i := strings.Index(lower[from:], name)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(name)
		if !asciiWordAt(lower, start-1) && !asciiWordAt(lower, end) {
			return true
		}
		from = start + 1
	}
}

func asciiWordAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	r := rune(s[i])
	return r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

func labelOf(id string) (string, bool) {
	i := slices.IndexFunc(providers, func(p provider) bool { return p.ID == id })
	if i < 0 {
		return "", false
	}
	return providers[i].Label, true
}

// IDs are the providers whose key the chat can ask for, in the order of the settings screen.
func IDs() []string {
	ids := make([]string, len(providers))
	for i, p := range providers {
		ids[i] = p.ID
	}
	return ids
}

// Label is the name of a provider as the settings screen shows it.
func Label(id string) (string, bool) { return labelOf(id) }
