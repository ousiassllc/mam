package logging

import (
	"regexp"
	"strings"
)

// redacted replaces a secret in what Read returns.
const redacted = "[redacted]"

// The logs are written without secrets (the key store, the token service and the adapters never pass a
// value to the logger), so this is the second line of defence for what the log reader hands to a tool: a
// secret that got into a line by mistake (an error text with a URL, a header dump) is not returned. The
// patterns are the shapes of the credentials mam handles: its own MCP tokens, Hugging Face tokens, the
// vendors' API keys, bearer credentials and JWTs, and the value of a key that is named like a secret.
var secretPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`), "$1 " + redacted},
	{regexp.MustCompile(`mam_mcp_[A-Za-z0-9_-]{8,}`), redacted},
	{regexp.MustCompile(`\bhf_[A-Za-z0-9]{16,}`), redacted},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}`), redacted},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{20,}`), redacted},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`), redacted},
	{regexp.MustCompile(`(?i)\b((?:x-goog-api-key|x-api-key|api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|id[_-]?token|client[_-]?secret|secret|password|passwd|token|authorization)["']?\s*[:=]\s*["']?)[^\s"'&,;}]+`), "${1}" + redacted},
	{regexp.MustCompile(`(?i)([?&]key=)[^\s&"']+`), "${1}" + redacted},
}

// sensitiveKey names an attribute whose string value is never returned.
var sensitiveKey = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|passwd|authorization|credential)`)

// minSecretLen is the shortest literal secret hidden: a shorter value would also hide ordinary words.
const minSecretLen = 8

// Redact hides the secrets in s: first the literal values the caller knows (the keys in the key store; a
// secret in a shape no pattern knows is still hidden), then the shapes.
func Redact(s string, known ...string) string {
	if s == "" {
		return s
	}
	for _, k := range known {
		if len(k) >= minSecretLen {
			s = strings.ReplaceAll(s, k, redacted)
		}
	}
	for _, p := range secretPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

// redactAttr returns the value of the attribute key as it may be shown: strings are scanned, and a string
// under a key named like a secret is replaced altogether. Numbers and booleans pass as they are.
func redactAttr(key string, v any, known []string) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	if sensitiveKey.MatchString(key) && strings.TrimSpace(s) != "" {
		return redacted
	}
	return Redact(s, known...)
}
