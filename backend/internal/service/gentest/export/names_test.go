package export

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNamePartLeavesNothingThatCanBeAPathOrADotName(t *testing.T) {
	for in, want := range map[string]string{
		"chroma1-hd":              "chroma1-hd",
		"runway-gen-4.5":          "runway-gen-4.5",
		"../../etc/passwd":        "etc-passwd",
		`..\..\windows\system32`:  "windows-system32",
		"box/plain":               "box-plain",
		"a b:c*d?e\"f<g>h|i":      "a-b-c-d-e-f-g-h-i",
		"モデル":                     "model",
		"..":                      "model",
		"":                        "model",
		"-.-":                     "model",
		"x\x00y\ny":               "x-y-y",
		"openai-compat:box/plain": "openai-compat-box-plain",
	} {
		got := namePart(in, modelNameMax, "model")
		assert.Equal(t, want, got, "%q", in)
		assert.NotContains(t, got, "/")
		assert.NotContains(t, got, `\`)
		assert.False(t, strings.HasPrefix(got, "."), "%q", got)
	}
	long := namePart(strings.Repeat("a", 500), modelNameMax, "model")
	assert.Len(t, long, modelNameMax, "a long model id is cut")
}

func TestFileNameIsNumberTimeModelAndTheEndOfTheID(t *testing.T) {
	at := time.Date(2026, 11, 1, 8, 38, 5, 0, time.UTC) // 17:38:05 in JST
	got := fileName(1, 40, at, "chroma1-hd", "0192f6c1-aaaa-7bbb-8ccc-123456bce291", "gentest/b/r.PNG", "image/png")
	assert.Equal(t, "01_20261101-173805_chroma1-hd_bce291.png", got)
	assert.Equal(t, "007_20261101-173805_chroma1-hd_bce291.png", fileName(7, 120, at, "chroma1-hd", "x-bce291", "r.png", ""), "the number is as wide as the largest one")
	hostile := fileName(2, 3, at, "../../x", "id/../..", "r.p/ng", "")
	assert.NotContains(t, hostile, "/")
	assert.NotContains(t, hostile, "..")
	assert.Equal(t, "bin", extension("noext", "application/x-unknown"))
	assert.Equal(t, "mp4", extension("noext", "video/mp4; codecs=avc1"))
	assert.Equal(t, "bin", extension("a.p/ng$", ""), "an extension with odd characters is not used")
}

func TestExtensionOfAGIFAndAnSVGResult(t *testing.T) {
	assert.Equal(t, "gif", extension("", "image/gif"))
	assert.Equal(t, "svg", extension("", "image/svg+xml"))
	assert.Equal(t, "svg", extension("b/r.svg", "image/svg+xml"))
}
