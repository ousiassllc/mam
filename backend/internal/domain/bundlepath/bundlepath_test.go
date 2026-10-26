package bundlepath_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain/bundlepath"
)

func TestValidID_AcceptsOnlyIDsSafeAsDirectoryHeaderAndURL(t *testing.T) {
	for _, id := range []string{"p1", "0b9f1a52-3c44-4e7e-9d6a-1c2d3e4f5a6b", "a_b-C", "reviews2", "console", "com0", "com10", strings.Repeat("x", 64)} {
		assert.True(t, bundlepath.ValidID(id), id)
	}
	for _, id := range []string{
		"", ".", "..", ".previews", ".staging", ".import-x", bundlepath.ReviewInboxDir, "Reviews", "REVIEWS", "con", "NUL", "Aux", "prn", "COM1", "com9", "lpt1", "LPT9",
		"a/b", `a\b`, "a:b", "a\x00b", "a\nb", `a"b`, "a?b", "a#b", "a b", "100%", "日本語", strings.Repeat("x", 65),
	} {
		assert.False(t, bundlepath.ValidID(id), "%q", id)
	}
}

func TestValidShape_AcceptsReservedNamesButNotOtherShapes(t *testing.T) {
	for _, id := range []string{"p1", "nul", "Reviews", "COM1", strings.Repeat("x", 64)} {
		assert.True(t, bundlepath.ValidShape(id), id)
	}
	for _, id := range []string{"", "a/b", "a.b", "a b", "日本語", strings.Repeat("x", 65)} {
		assert.False(t, bundlepath.ValidShape(id), "%q", id)
	}
}

func TestValidFilePath_AcceptsOrdinaryNamesBelowTheProject(t *testing.T) {
	for _, p := range []string{"p/a.mp4", "p/takes/t1.mp4", "p/a b.png", "p/.hidden", "p/console.txt", "p/com0", "p/com10.mp4", "p/nul_1.mp4", "p/日本語.png", "p/a.b.c", "p/console/a.mp4"} {
		assert.True(t, bundlepath.ValidFilePath("p", p), p)
	}
}

// Windows cannot create these names (#725), as it cannot create a Project directory of the same names (#689).
func TestValidFilePath_RefusesNamesWindowsCannotCreate(t *testing.T) {
	for _, p := range []string{
		"p/NUL.mp4", "p/a/con", "p/aux.txt", "p/PRN", "p/Com1.tar.gz", "p/a/LPT9.png", "p/nul",
		"p/COM¹.png", "p/COM².mp4", "p/com³", "p/lpt¹", "p/LPT².mp4", "p/a/lpt³.tar.gz",
		"p/x*y", "p/a?b", `p/a"b`, "p/a<b", "p/a>b", "p/a|b", "p/a\tb", "p/a\nb", "p/a\x7fb", "p/a/\x01",
		"p/a.", "p/a /b", "p/a/b ", "p/a/b.", "p/a./b",
		"p", "p/", "p//a", "p/../a", "p/a/..", "/p/a", `p\a`, "p/a:b", "q/a.mp4",
	} {
		assert.False(t, bundlepath.ValidFilePath("p", p), "%q", p)
	}
}

// NTFS and APFS ignore letter case, and a file cannot also be a directory (#735).
func TestConflict(t *testing.T) {
	for name, paths := range map[string][]string{
		"letter case only":       {"p/x.mp4", "p/X.mp4"},
		"file and directory":     {"p/x", "p/x/y.mp4"},
		"file and deep dir":      {"p/x/y/z.mp4", "p/x"},
		"file and dir, any case": {"p/X", "p/x/y.mp4"},
	} {
		assert.True(t, bundlepath.Conflict(paths), name)
	}
	for name, paths := range map[string][]string{
		"none":                           nil,
		"distinct names":                 {"p/a.mp4", "p/b.mp4", "p/takes/a.mp4"},
		"same spelling twice":            {"p/a.mp4", "p/a.mp4"},
		"directory spelled in two cases": {"p/Takes/a.mp4", "p/takes/b.mp4"},
		"shared directory":               {"p/t/a.mp4", "p/t/b.mp4"},
		"prefix of a name":               {"p/x", "p/xy/z.mp4"},
	} {
		assert.False(t, bundlepath.Conflict(paths), name)
	}
}
