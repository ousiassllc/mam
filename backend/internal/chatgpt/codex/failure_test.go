package codex

import (
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

func TestKindOfFollowsTheErrorTable(t *testing.T) {
	cases := map[string]Kind{
		protocol.ErrUsageLimitExceeded:           KindUsageLimit,
		protocol.ErrUnauthorized:                 KindUnauthorized,
		protocol.ErrHTTPConnectionFailed:         KindTransient,
		protocol.ErrResponseStreamConnectFailed:  KindTransient,
		protocol.ErrResponseStreamDisconnected:   KindTransient,
		protocol.ErrResponseTooManyFailedAttempt: KindTransient,
		protocol.ErrInternalServerError:          KindTransient,
		protocol.ErrServerOverloaded:             KindTransient,
		protocol.ErrRateLimitExceeded:            KindTransient,
		protocol.ErrContextWindowExceeded:        KindPermanent,
		protocol.ErrBadRequest:                   KindPermanent,
		protocol.ErrSandboxError:                 KindPermanent,
		protocol.ErrOther:                        KindPermanent,
		"cyberPolicy":                            KindPermanent,
		"aKindOfALaterVersion":                   KindPermanent,
		"":                                       KindPermanent,
	}
	for kind, want := range cases {
		if got := kindOf(protocol.ErrorInfo{Kind: kind}); got != want {
			t.Errorf("%q: %s, want %s", kind, got, want)
		}
	}
}

func TestSanitizeRemovesTokenLikeTextAndShortens(t *testing.T) {
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ4In0.c2lnbmF0dXJl"
	for _, in := range []string{
		"failed: " + jwt, "Authorization: Bearer abc.def-ghi", "key sk-abcdefghijklmnopqrstuvwxyz0123", "refresh rt_abcdefghijklmnopqrstuvwx",
	} {
		out := sanitize(in)
		if !strings.Contains(out, "[redacted]") || strings.Contains(out, "eyJ") || strings.Contains(out, "abcdefghijklmnop") || strings.Contains(out, "abc.def") {
			t.Errorf("sanitize(%q) = %q", in, out)
		}
	}
	long := sanitize(strings.Repeat("あ", 1000))
	if r := []rune(long); len(r) != maxMessageRunes+1 {
		t.Errorf("length = %d", len(r))
	}
	if got := sanitize("  plain message "); got != "plain message" {
		t.Errorf("sanitize = %q", got)
	}
}

func TestFormatResetIsJapanStandardTime(t *testing.T) {
	got := FormatReset(time.Date(2026, 10, 25, 9, 30, 0, 0, time.UTC))
	if got != "2026-10-25 18:30（日本時間）" {
		t.Errorf("got %q", got)
	}
}
