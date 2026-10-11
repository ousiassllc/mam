package httpclient

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// snippetRunes is how many runes of an error body Snippet keeps.
const snippetRunes = 300

// Snippet is the start of a vendor error body, for an error message: whitespace runs (newlines
// included) collapse to one space, and a body cut at snippetRunes runes ends with "…".
func Snippet(b []byte) string {
	return domain.TruncateRunesEllipsis(strings.Join(strings.Fields(string(b)), " "), snippetRunes)
}

// RedactURL drops the request URL from a transport error: it must not leak into logs or the chat.
func RedactURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// ParseRetryAfter reads a Retry-After header given in seconds; 0 when it is absent, not a positive
// number of seconds (an HTTP date is not supported) or invalid.
func ParseRetryAfter(v string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}
