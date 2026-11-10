// Package secretguard finds what looks like an API key in a chat message. A key must never be a chat message (it would stay in the history, the events and every
// export of the Project, and be sent to the LLM that reads the message): the chat asks for it in the masked field of a card (#1140). A user who pastes one anyway has the
// message recorded with the key hidden, and is told where to put it.
package secretguard

import "regexp"

// Hidden replaces a key in the message that is recorded.
const Hidden = "［API キーらしき文字列は伏せました］"

// patterns are the shapes of the keys of the providers the settings screen manages (OpenAI / Anthropic / OpenRouter sk-…, ElevenLabs sk_…, Google AIza…, Hugging Face
// hf_…, Replicate r8_…, fal.ai id:secret, a Bearer token) and a long token that follows the word "key" or "token". A key it does not know the shape of is not found:
// the card is the way to enter one, this is the net under it.
var patterns = []*regexp.Regexp{
	regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{20,}`),
	regexp.MustCompile(`\bsk_[A-Za-z0-9]{32,}`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{30,}`),
	regexp.MustCompile(`\bhf_[A-Za-z0-9]{30,}`),
	regexp.MustCompile(`\br8_[A-Za-z0-9]{30,}`),
	regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}:[0-9a-f]{32}\b`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-]{24,}`),
	regexp.MustCompile(`(?i)(?:api[ _\-]?key|access[ _\-]?token|secret|キー|トークン)\s*(?:は|が|=|:|：|は\s*)?\s*[「"'` + "`" + `]?[A-Za-z0-9_\-]{32,}`),
}

// Redact returns text with every key hidden (Hidden) and whether it found one.
func Redact(text string) (string, bool) {
	found := false
	for _, p := range patterns {
		if p.MatchString(text) {
			found = true
			text = p.ReplaceAllString(text, Hidden)
		}
	}
	return text, found
}
