// Package secretguard finds what looks like an API key in a chat message. A key must never be a chat message (it would stay in the history, the events and every
// export of the Project, and be sent to the LLM that reads the message): the chat asks for it in the masked field of a card (#1140). A user who pastes one anyway has the
// message recorded with the key hidden, and is told where to put it.
package secretguard

import (
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
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
	regexp.MustCompile(`(?i)(?:api[ _\-]?key|access[ _\-]?token|secret|キー|トークン)\s*(?:は|が|=|:|：)?\s*[「"'` + "`" + `]?[A-Za-z0-9_\-]{32,}`),
}

// A key whose provider has no shape of its own (Kling's 32 letters and digits, Runway's key_ + hex, Luma's, Seedance's and Flux's UUID, Sync Labs') is found by what every
// key is: one long token of letters, digits and _ . - that holds both letters and digits, whether it is the whole message or stands beside the words of a sentence.
var (
	token     = regexp.MustCompile(`^[A-Za-z0-9_.\-]{24,}$`)
	tokenRun  = regexp.MustCompile(`[A-Za-z0-9_.\-]{24,}`)
	quoteEdge = "\"'`「」『』（）()[]<>＜＞"
)

// What a user writes that is long and has letters and digits, but is no key: the name of a file of a medium or a document ("scene01_shot02_take03_final_render.mp4": the
// extension is what says so; no other shape is used, as a key can be built like a file name without one of these extensions) and an ID the app made itself (its IDs are
// UUIDv7, domain.NewID; a key that is a UUID has another version: the UUIDs of Seedance and Flux are random ones).
var (
	fileName = regexp.MustCompile(`(?i)\.(?:mp4|m4v|mov|mkv|avi|webm|gif|png|jpe?g|webp|bmp|tiff?|heic|wav|mp3|m4a|flac|ogg|aac|srt|vtt|txt|md|json|csv|pdf|zip)$`)
	appID    = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// isNotKey reports whether the run is something that is long and made of letters and digits but is known not to be a key: a model of the catalog or its file, a file of a medium
// or a document, an ID of the app.
func isNotKey(run string) bool {
	run = strings.TrimRight(run, ".")
	return isModelName(run) || fileName.MatchString(run) || appID.MatchString(run)
}

// hasLetterAndDigit reports whether the run holds a letter and a digit (a key does; a long word or a hyphenated phrase does not).
func hasLetterAndDigit(run string) bool {
	return strings.IndexFunc(run, unicode.IsLetter) >= 0 && strings.IndexFunc(run, unicode.IsDigit) >= 0
}

// Redact returns text with every key hidden (Hidden) and whether it found one.
func Redact(text string) (string, bool) {
	if whole := strings.Trim(strings.TrimSpace(text), quoteEdge); token.MatchString(whole) && hasLetterAndDigit(whole) && !isNotKey(whole) {
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

// looksLikeKey reports whether the run text[from:to] is a key among words: it holds a letter and a digit, is not known not to be a key (isNotKey), and is not part of a path or a URL.
func looksLikeKey(text string, from, to int) bool {
	if (from > 0 && text[from-1] == '/') || (to < len(text) && text[to] == '/') {
		return false
	}
	run := text[from:to]
	return hasLetterAndDigit(run) && !isNotKey(run)
}

// modelNames are the IDs of the models of the catalog and the names of the files it pins (built once). A user says them to ask for a model ("ltx-2.5-22b-distilled-nvfp4 で作って",
// "ltx-2.5-audio-vae-bf16.safetensors を使って"; spec/spec.md §2.1), and they are long tokens of letters and digits that would be read as a key. Only a run that is exactly
// one of them is no key: no shape rule ("it is lowercase words and hyphens", "it ends with .safetensors") is used, as a key can be built like that.
var modelNames = sync.OnceValue(func() map[string]struct{} {
	names := map[string]struct{}{}
	for _, m := range catalog.Default().Models {
		names[m.ID] = struct{}{}
		for _, f := range m.Files {
			names[f.Name] = struct{}{}
		}
	}
	return names
})

// isModelName reports whether the run is exactly the ID of a model of the catalog or the name of one of its files (a trailing "." is the end of a sentence).
func isModelName(run string) bool {
	_, ok := modelNames()[strings.TrimRight(run, ".")]
	return ok
}

// OneToken reports whether text is a single word of printable ASCII (8 characters or more): what a user who types a key of a shape nobody knows into the chat while the card
// that asks for it is open sends.
//
// The name of a file, a model of the catalog and an ID of the app are not keys here either (what is said beside the card is read as it is). An English word of 8 letters or more
// is one: a word and a key of a shape nobody knows cannot be told apart, and the card stays open for the key.
func OneToken(text string) bool {
	text = strings.Trim(strings.TrimSpace(text), quoteEdge)
	return len(text) >= 8 && strings.IndexFunc(text, func(r rune) bool { return r <= ' ' || r > '~' }) < 0 && !isNotKey(text)
}
