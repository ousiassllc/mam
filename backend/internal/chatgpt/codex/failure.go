package codex

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// Kind says what a failed call needs, which decides how the caller treats it (retry, fail over, tell the user).
type Kind string

// The kinds of failure.
const (
	// KindUsageLimit: the plan's usage limit is reached. Not retried, not counted by a circuit breaker; ResetsAt may say when it ends.
	KindUsageLimit Kind = "usage_limit"
	// KindNotSignedIn and KindUnauthorized: the user must sign in again.
	KindNotSignedIn  Kind = "not_signed_in"
	KindUnauthorized Kind = "unauthorized"
	// KindEngine: the engine is not installed (or the path set is gone): the user must install or set it.
	KindEngine Kind = "engine"
	// KindUnsupported: the request cannot be done with this model (a picture to a text-only model).
	KindUnsupported Kind = "unsupported"
	// KindTransient: a connection, stream or server trouble, or the process ending: worth another try.
	KindTransient Kind = "transient"
	// KindPermanent: waiting does not help (context window, bad request, sandbox error, other, a turn that used a tool).
	KindPermanent Kind = "permanent"
)

// Failure is a failed call of the app-server. Message holds no token (see sanitize).
type Failure struct {
	Kind    Kind
	Message string
	// ResetsAt is when a usage limit is lifted (zero when unknown).
	ResetsAt time.Time
	Err      error
}

func (f *Failure) Error() string {
	if f.Err != nil && f.Message == "" {
		return fmt.Sprintf("codex: %s: %v", f.Kind, f.Err)
	}
	return fmt.Sprintf("codex: %s: %s", f.Kind, f.Message)
}

// Unwrap returns the cause.
func (f *Failure) Unwrap() error { return f.Err }

// ErrEngineNotInstalled means there is no app-server to start: nothing is installed and no path is set.
var ErrEngineNotInstalled = errors.New("codex: the app-server is not installed")

// ErrEngineInstalling means the engine is being installed: its files are replaced, so it is not started until the install ends.
var ErrEngineInstalling = errors.New("codex: the engine is being installed")

// ErrEngineRemoving means the engine and mam's Codex files are being deleted (a logout with removal): nothing is installed under the
// deletion, so an install is refused until it ends.
var ErrEngineRemoving = errors.New("codex: the engine is being removed")

// ErrUnsupported means the download is not available on this platform (only Windows and macOS on Apple Silicon have
// it): set the path of a codex.
var ErrUnsupported = errors.New("codex: the engine download is available on Windows and macOS (Apple Silicon) only; set the path of an installed codex")

// kindOf is the error table of the Issue (#845 §4): what each codexErrorInfo means for the caller.
func kindOf(info protocol.ErrorInfo) Kind {
	switch info.Kind {
	case protocol.ErrUsageLimitExceeded:
		return KindUsageLimit
	case protocol.ErrUnauthorized:
		return KindUnauthorized
	case protocol.ErrHTTPConnectionFailed, protocol.ErrResponseStreamConnectFailed, protocol.ErrResponseStreamDisconnected,
		protocol.ErrResponseTooManyFailedAttempt, protocol.ErrInternalServerError, protocol.ErrServerOverloaded, protocol.ErrRateLimitExceeded:
		return KindTransient
	}
	// ContextWindowExceeded, BadRequest, SandboxError, Other and every kind this table does not know: not retried.
	return KindPermanent
}

const maxMessageRunes = 300

// sanitize makes a message of the app-server fit to show: anything that looks like a token is removed and the text is
// shortened. The app-server does not print tokens; this is the second guard.
func sanitize(msg string) string {
	msg = appserver.Redact(strings.TrimSpace(msg))
	if r := []rune(msg); len(r) > maxMessageRunes {
		msg = string(r[:maxMessageRunes]) + "…"
	}
	return msg
}

// JST is the time zone the reset time is shown in (Japan Standard Time has no daylight saving time).
var JST = time.FixedZone("JST", 9*60*60)

// FormatReset is a reset time as the user reads it: "2026-10-25 18:30（日本時間）".
func FormatReset(t time.Time) string {
	return t.In(JST).Format("2006-01-02 15:04") + "（日本時間）"
}
