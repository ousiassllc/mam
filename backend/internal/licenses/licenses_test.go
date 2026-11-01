package licenses_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam"
	"github.com/ousiassllc/mam/backend/internal/licenses"
)

// The shape of the generated file, in small: a preamble, the table of all components (left out), a component with two license
// files (its kind line becomes its group), a license whose text holds a "## " line and a longer fence, and a hand-written section.
const sample = "# 表題\r\n\r\nはじめの文。\r\n\r\n## 一覧\r\n\r\n| 種別 | 名前 |\r\n|---|---|\r\n| Go モジュール | a |\r\n\r\n" +
	"## example.com/a v1.0.0\r\n\r\nGo モジュール\r\n\r\n### LICENSE\r\n\r\n~~~\r\nlicense of a\r\n## not a heading\r\n~~~\r\n\r\n### LICENSE.md\r\n\r\n~~~~\r\nuses ~~~ inside\r\n~~~\r\n~~~~\r\n\r\n" +
	"## ComfyUI（利用者が取得する別プログラム）\r\n\r\n別のプログラムです。<script>alert(1)</script>\r\n"

func TestParseNotices_SplitsTheGeneratedFileIntoEntries(t *testing.T) {
	got := licenses.ParseNotices(sample)
	require.Len(t, got, 3)

	assert.Equal(t, licenses.Entry{Group: "概要", Title: "このページについて", Body: "# 表題\n\nはじめの文。"}, got[0])

	assert.Equal(t, "Go モジュール", got[1].Group, "the kind line is the group")
	assert.Equal(t, "example.com/a v1.0.0", got[1].Title)
	assert.Equal(t, "### LICENSE\n\nlicense of a\n## not a heading\n\n### LICENSE.md\n\nuses ~~~ inside\n~~~", got[1].Body, "fence lines are dropped, the text between them is kept whole")

	assert.Equal(t, licenses.NoticeGroup, got[2].Group, "a section without a kind line is a hand-written one")
	assert.Equal(t, "ComfyUI（利用者が取得する別プログラム）", got[2].Title)
	assert.Equal(t, "別のプログラムです。<script>alert(1)</script>", got[2].Body, "the body is text as it is: escaping is the screen's job")
}

func TestParseNotices_AFileWithoutSectionsIsTheOneEntryOfItsPreamble(t *testing.T) {
	assert.Equal(t, []licenses.Entry{{Group: "概要", Title: "このページについて", Body: "text"}}, licenses.ParseNotices("text\n"))
	assert.Empty(t, licenses.ParseNotices(""))
}

func TestNew_GroupsEntriesByFirstAppearanceAndNumbersThem(t *testing.T) {
	c := licenses.New(
		func() []licenses.Entry {
			return []licenses.Entry{{Group: "B", Title: "b1"}, {Group: "A", Title: "a1"}, {Group: "B", Title: "b2"}}
		},
		func() []licenses.Entry {
			return []licenses.Entry{{Title: "no group", Body: "x"}, {Group: "A", Title: "a2"}}
		},
	)
	require.Len(t, c.Groups(), 3)
	names := []string{}
	for _, g := range c.Groups() {
		names = append(names, g.Name)
	}
	assert.Equal(t, []string{"B", "A", licenses.DefaultGroup}, names)
	assert.Equal(t, 5, c.Len())

	b := c.Groups()[0].Sections
	require.Len(t, b, 2)
	assert.Equal(t, []string{"b1", "b2"}, []string{b[0].Title, b[1].Title})
	s, ok := c.Find(b[1].ID)
	require.True(t, ok)
	assert.Equal(t, "b2", s.Title)
	_, ok = c.Find("0")
	assert.False(t, ok)
	_, ok = c.Find("99")
	assert.False(t, ok)
	_, ok = c.Find("")
	assert.False(t, ok)
}

// rootNotices is THIRD_PARTY_NOTICES.md of the repository root, read from disk.
func rootNotices(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "THIRD_PARTY_NOTICES.md"))
	require.NoError(t, err)
	return string(b)
}

// The screen shows the bytes of the file the installer ships and the script writes: the embed reads the repository root,
// and this test fails if that ever stops being so (an embed of a copy). That the file is what the script writes is
// `make check-notices` (scripts/check-third-party-notices.sh, run by CI).
func TestEmbeddedNotices_AreTheFileOfTheRepositoryRoot(t *testing.T) {
	root := rootNotices(t)
	assert.NotEmpty(t, root)
	assert.True(t, root == mam.ThirdPartyNotices, "the embedded THIRD_PARTY_NOTICES.md differs from the file of the repository root: rebuild")
}

// The real file: every component of the generated part and every hand-written section is an entry, with the text of its
// license, and the sections of the local runtimes (GPT-SoVITS, MuseTalk) are entries of their own.
func TestThirdPartyNotices_EveryHeadingOfTheFileIsAnEntryWithItsText(t *testing.T) {
	entries := licenses.ThirdPartyNotices()

	var headings []string
	fence := ""
	for _, line := range strings.Split(rootNotices(t), "\n") {
		if strings.Trim(line, "~") == "" && len(line) >= 3 {
			switch fence {
			case "":
				fence = line
			case line:
				fence = ""
			}
			continue
		}
		if fence == "" && strings.HasPrefix(line, "## ") && line != "## 一覧" {
			headings = append(headings, strings.TrimPrefix(line, "## "))
		}
	}
	require.NotEmpty(t, headings)
	titles := []string{}
	for _, e := range entries {
		assert.NotEmpty(t, strings.TrimSpace(e.Body), e.Title)
		assert.NotEmpty(t, e.Group, e.Title)
		titles = append(titles, e.Title)
	}
	assert.Equal(t, append([]string{"このページについて"}, headings...), titles, "the entries are the sections of the file, in its order (the table 一覧 left out)")

	byTitle := map[string]licenses.Entry{}
	for _, e := range entries {
		byTitle[e.Title] = e
	}
	onnx := byTitle["ONNX Runtime（onnxruntime.dll） 1.23.2"]
	assert.Equal(t, "同梱した素材", onnx.Group)
	assert.Contains(t, onnx.Body, "MIT License")
	assert.Contains(t, onnx.Body, "ThirdPartyNotices.txt")
	assert.Equal(t, "Go 標準ライブラリ", byTitle["Go standard library"].Group)
	assert.Equal(t, "Go モジュール", byTitle["github.com/a-h/templ v0.3.1070"].Group)
	assert.Equal(t, "npm パッケージ", byTitle["lit 3.3.3"].Group)
	for _, title := range []string{"GPT-SoVITS（利用者が取得する別プログラム）", "MuseTalk 1.5（利用者が取得する別プログラム）", "GPT-SoVITS と MuseTalk の重み・依存ファイル（利用者が取得するもの）"} {
		e, ok := byTitle[title]
		assert.True(t, ok, title)
		assert.Equal(t, licenses.NoticeGroup, e.Group, title)
	}
	assert.NotContains(t, byTitle, "一覧")
}

func TestDefault_IsTheCatalogOfTheNotices(t *testing.T) {
	c := licenses.Default()
	assert.Same(t, c, licenses.Default())
	assert.Equal(t, len(licenses.ThirdPartyNotices()), c.Len())
	assert.Equal(t, "概要", c.Groups()[0].Name)
}
