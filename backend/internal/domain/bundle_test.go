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

func TestValidBundleFilePath_AcceptsOrdinaryNamesBelowTheProject(t *testing.T) {
	for _, p := range []string{"p/a.mp4", "p/takes/t1.mp4", "p/a b.png", "p/.hidden", "p/console.txt", "p/com0", "p/com10.mp4", "p/nul_1.mp4", "p/日本語.png", "p/a.b.c", "p/console/a.mp4"} {
		assert.True(t, domain.ValidBundleFilePath("p", p), p)
	}
}

// Windows cannot create these names (#725), as it cannot create a Project directory of the same names (#689).
func TestValidBundleFilePath_RefusesNamesWindowsCannotCreate(t *testing.T) {
	for _, p := range []string{
		"p/NUL.mp4", "p/a/con", "p/aux.txt", "p/PRN", "p/Com1.tar.gz", "p/a/LPT9.png", "p/nul",
		"p/x*y", "p/a?b", `p/a"b`, "p/a<b", "p/a>b", "p/a|b", "p/a\tb", "p/a\nb", "p/a\x7fb", "p/a/\x01",
		"p/a.", "p/a /b", "p/a/b ", "p/a/b.", "p/a./b",
		"p", "p/", "p//a", "p/../a", "p/a/..", "/p/a", `p\a`, "p/a:b", "q/a.mp4",
	} {
		assert.False(t, domain.ValidBundleFilePath("p", p), "%q", p)
	}
}
