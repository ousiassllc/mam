package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestValidBundleID_AcceptsOnlyIDsSafeAsDirectoryHeaderAndURL(t *testing.T) {
	for _, id := range []string{"p1", "0b9f1a52-3c44-4e7e-9d6a-1c2d3e4f5a6b", "a_b-C", "reviews2", "console", "com0", "com10", strings.Repeat("x", 64)} {
		assert.True(t, domain.ValidBundleID(id), id)
	}
	for _, id := range []string{
		"", ".", "..", ".previews", ".staging", ".import-x", domain.ReviewInboxDir, "Reviews", "REVIEWS", "con", "NUL", "Aux", "prn", "COM1", "com9", "lpt1", "LPT9",
		"a/b", `a\b`, "a:b", "a\x00b", "a\nb", `a"b`, "a?b", "a#b", "a b", "100%", "日本語", strings.Repeat("x", 65),
	} {
		assert.False(t, domain.ValidBundleID(id), "%q", id)
	}
}
