package httpclient_test

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

func TestSnippet(t *testing.T) {
	t.Run("collapses newlines and runs of whitespace", func(t *testing.T) {
		assert.Equal(t, "a b c", httpclient.Snippet([]byte("  a\n\n b\t\t c \n")))
	})

	t.Run("a short body is returned whole, with no ellipsis", func(t *testing.T) {
		assert.Equal(t, "エラー", httpclient.Snippet([]byte("エラー")))
	})

	t.Run("a long multi-byte body is cut on a rune boundary and marked", func(t *testing.T) {
		got := httpclient.Snippet([]byte(strings.Repeat("あ", 400)))
		assert.True(t, utf8.ValidString(got))
		assert.Equal(t, strings.Repeat("あ", 300)+"…", got)
	})

	t.Run("a body of exactly 300 runes is not marked as cut", func(t *testing.T) {
		body := strings.Repeat("あ", 300)
		assert.Equal(t, body, httpclient.Snippet([]byte(body)))
	})
}

func TestRedactURL(t *testing.T) {
	inner := errors.New("connection refused")
	got := httpclient.RedactURL(&url.Error{Op: "Post", URL: "https://x.test/path?sig=secret", Err: inner})
	assert.Same(t, inner, got)
	assert.NotContains(t, got.Error(), "secret")

	plain := errors.New("boom")
	assert.Same(t, plain, httpclient.RedactURL(plain))
}

func TestParseRetryAfter(t *testing.T) {
	assert.Equal(t, 7*time.Second, httpclient.ParseRetryAfter(" 7 "))
	assert.Zero(t, httpclient.ParseRetryAfter(""))
	assert.Zero(t, httpclient.ParseRetryAfter("0"))
	assert.Zero(t, httpclient.ParseRetryAfter("-3"))
	assert.Zero(t, httpclient.ParseRetryAfter("Wed, 21 Oct 2026 07:28:00 GMT"))
}
