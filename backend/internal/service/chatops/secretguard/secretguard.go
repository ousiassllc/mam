// Package secretguard finds what looks like an API key in a chat message. A key must never be a chat message (it would stay in the history, the events and every
// export of the Project, and be sent to the LLM that reads the message): the chat asks for it in the masked field of a card (#1140). A user who pastes one anyway has the
// message recorded with the key hidden, and is told where to put it.
package secretguard

import (
	"regexp"
	"strings"
	"unicode"
)

// Hidden replaces a key in the message that is recorded.
const Hidden = "［API キーらしき文字列は伏せました］"

// patterns are the shapes of the keys of the providers the settings screen manages (OpenAI / Anthropic / OpenRouter sk-…, ElevenLabs sk_…, Google AIza…, Hugging Face
// hf_…, Replicate r8_…, fal.ai id:secret, a Bearer token, a JWT such as MiniMax's) and a long token that follows the word "key" or "token".
var patterns = []*regexp.Regexp{
	regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{20,}`),
	regexp.MustCompile(`\bsk_[A-Za-z0-9]{32,}`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{30,}`),
	regexp.MustCompile(`\bhf_[A-Za-z0-9]{30,}`),
	regexp.MustCompile(`\br8_[A-Za-z0-9]{30,}`),
	regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}:[0-9a-f]{32}\b`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-]{24,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{6,}\.[A-Za-z0-9_\-]{6,}\.[A-Za-z0-9_\-]{6,}`),
	regexp.MustCompile(`(?i)(?:api[ _\-]?key|access[ _\-]?token|secret|キー|トークン)\s*(?:は|が|=|:|：|は\s*)?\s*[「"'` + "`" + `]?[A-Za-z0-9_\-]{32,}`),
}

// A key whose provider has no shape of its own (Kling's 32 letters and digits, Runway's key_ + hex, Luma's, Seedance's and Flux's UUID, Sync Labs') is found by what every
// key is: one long token of letters, digits and _ . - that is the whole message, or that holds letters and digits beside the words of a sentence.
var (
	token     = regexp.MustCompile(`^[A-Za-z0-9_.\-]{24,}$`)
	tokenRun  = regexp.MustCompile(`[A-Za-z0-9_.\-]{24,}`)
	quoteEdge = "\"'`「」『』（）()[]<>＜＞"
)

// Redact returns text with every key hidden (Hidden) and whether it found one.
func Redact(text string) (string, bool) {
	if whole := strings.Trim(strings.TrimSpace(text), quoteEdge); token.MatchString(whole) && !isModelName(whole) {
		return Hidden, true
	}
	found := false
	for _, p := range patterns {
		if p.MatchString(text) {
			found = true
			text = p.ReplaceAllString(text, Hidden)
		}
	}
	var out strings.Builder
	last := 0
	for _, at := range tokenRun.FindAllStringIndex(text, -1) {
		if !looksLikeKey(text, at[0], at[1]) {
			continue
		}
		out.WriteString(text[last:at[0]] + Hidden)
		last, found = at[1], true
	}
	return out.String() + text[last:], found
}

// looksLikeKey reports whether the run text[from:to] is a key among words: it holds a letter and a digit, and is not part of a path or a URL.
func looksLikeKey(text string, from, to int) bool {
	if (from > 0 && text[from-1] == '/') || (to < len(text) && text[to] == '/') {
		return false
	}
	run := text[from:to]
	if isModelName(run) {
		return false
	}
	return strings.IndexFunc(run, unicode.IsLetter) >= 0 && strings.IndexFunc(run, unicode.IsDigit) >= 0
}

// modelFileExts are the endings of the model files the catalog names (Hugging Face / ComfyUI): a key never ends with one.
var modelFileExts = []string{".safetensors", ".gguf", ".ckpt", ".pt", ".pth", ".bin", ".onnx"}

// isModelName reports whether the run is the name of a model, which a user says when they ask for it ("ltx-2.5-22b-distilled-nvfp4 で作って"; spec/spec.md §2.1), not a
// key: a file of a model (it ends with .safetensors, .gguf…), or an ID of the catalog (lowercase words, versions and sizes joined by hyphens: at least three parts, one
// of them a word of four letters or more that no hex digits alone spell, and none of them a long run of letters and digits, as a key's chunk is). What is no key by
// these rules is still read by patterns (sk-…, hf_…, a UUID…), which run before and apart from this.
func isModelName(run string) bool {
	run = strings.TrimRight(run, ".")
	lower := strings.ToLower(run)
	for _, ext := range modelFileExts {
		if strings.HasSuffix(lower, ext) && len(run) > len(ext) {
			return true
		}
	}
	if run != lower || strings.ContainsAny(run, "_") {
		return false
	}
	parts := strings.Split(run, "-")
	if len(parts) < 3 {
		return false
	}
	word := false
	for _, p := range parts {
		switch {
		case p == "" || len(p) > 16:
			return false
		case len(p) > 8 && strings.IndexFunc(p, unicode.IsLetter) >= 0 && strings.IndexFunc(p, unicode.IsDigit) >= 0:
			return false
		case len(p) >= 4 && strings.IndexFunc(p, func(r rune) bool { return r < 'a' || r > 'z' }) < 0 && strings.IndexFunc(p, func(r rune) bool { return r > 'f' }) >= 0:
			word = true
		}
	}
	return word
}

// OneToken reports whether text is a single word of printable ASCII (8 characters or more): what a user who types a key of a shape nobody knows into the chat while the card
// that asks for it is open sends.
func OneToken(text string) bool {
	text = strings.Trim(strings.TrimSpace(text), quoteEdge)
	return len(text) >= 8 && strings.IndexFunc(text, func(r rune) bool { return r <= ' ' || r > '~' }) < 0
}
