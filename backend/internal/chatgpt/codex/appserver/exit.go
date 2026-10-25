package appserver

import (
	"fmt"
	"regexp"
	"strings"
)

// tokenLike matches what looks like a credential: a JWT, an OAuth client or refresh token, an API key.
var tokenLike = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}(\.[A-Za-z0-9_-]+){0,2}|\b(?:sk|oaiapp|rt|at)[-_][A-Za-z0-9_-]{16,}|\b[Bb]earer\s+\S+`)

// Redact removes what looks like a credential from text of the app-server that is shown to the user.
func Redact(s string) string { return tokenLike.ReplaceAllString(s, "[redacted]") }

// exitText is the sentence about a process that ended: the exit code and the end of its standard error. The app-server
// prints no token, but a user-set RUST_LOG could make it log more: the tail is redacted like every other text of it.
func exitText(code int, tail string) string {
	msg := fmt.Sprintf("Codex app-server が終了しました（終了コード %d）", code)
	if t := strings.TrimSpace(tail); t != "" {
		msg += "\n--- 標準エラーの末尾 ---\n" + Redact(t)
	}
	return msg
}
