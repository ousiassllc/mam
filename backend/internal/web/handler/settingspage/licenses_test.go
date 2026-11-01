package settingspage_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/licenses"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

var entryRe = regexp.MustCompile(`(?s)<details[^>]*hx-get="(/settings/licenses/[^"]+)"[^>]*>\s*<summary[^>]*>([^<]*)</summary>`)

// The page lists the entries of the notices in groups, and holds titles only: the text of an entry is read when it is opened.
func TestLicenses_PageListsTheGroupsAndEntriesWithoutTheirText(t *testing.T) {
	s := newScreen(t, nil)
	rec := s.get(paths.SettingsCategory(paths.SettingsLicenses))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	for _, group := range []string{"概要", "Go 標準ライブラリ", "Go モジュール", "npm パッケージ", "同梱した素材", licenses.NoticeGroup} {
		assert.Contains(t, body, ">"+group+" <span", group)
	}
	entries := entryRe.FindAllStringSubmatch(body, -1)
	require.Len(t, entries, licenses.Default().Len())
	titles := map[string]string{}
	for _, e := range entries {
		titles[e[2]] = e[1]
	}
	for _, title := range []string{"ONNX Runtime（onnxruntime.dll） 1.23.2", "GPT-SoVITS（利用者が取得する別プログラム）", "MuseTalk 1.5（利用者が取得する別プログラム）"} {
		assert.Contains(t, titles, title)
	}
	assert.Contains(t, body, `hx-trigger="toggle once"`)
	assert.Less(t, len(body), 200_000, "the 13000 lines of the notices are not in the page")
	assert.NotContains(t, body, "Permission is hereby granted", "no license text in the page")
	assert.Contains(t, body, `aria-label="第三者のライセンスの説明"`, "the explanation is behind the ? button")
}

// The overview has a card for the category, with the number of entries.
func TestLicenses_OverviewHasACard(t *testing.T) {
	s := newScreen(t, nil)
	body := s.get(paths.Settings()).Body.String()
	assert.Contains(t, body, `href="`+paths.SettingsCategory(paths.SettingsLicenses)+`"`)
	assert.Contains(t, body, "第三者のライセンス")
}

// An entry's text: a fragment for HTMX, escaped, and the text of the file; other requests get the page; an unknown id is 404.
func TestLicenses_TextOfAnEntry(t *testing.T) {
	s := newScreen(t, nil)
	page := s.get(paths.SettingsCategory(paths.SettingsLicenses)).Body.String()
	var url string
	for _, e := range entryRe.FindAllStringSubmatch(page, -1) {
		if e[2] == "GPT-SoVITS（利用者が取得する別プログラム）" {
			url = e[1]
		}
	}
	require.NotEmpty(t, url)

	rec := s.get(url, "HX-Request", "true")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "<html")
	assert.Contains(t, rec.Body.String(), `data-testid="license-text"`)
	assert.Contains(t, rec.Body.String(), "https://github.com/RVC-Boss/GPT-SoVITS/tree/20250606v2pro", "the provenance of GPT-SoVITS")
	assert.Contains(t, rec.Header().Values("Vary"), "HX-Request, HX-History-Restore-Request")

	nav := s.get(url)
	require.Equal(t, http.StatusOK, nav.Code)
	assert.Contains(t, nav.Body.String(), "<html", "an address typed in shows the page of the category")
	assert.Equal(t, 1, strings.Count(nav.Body.String(), `id="settings-category"`))

	for _, id := range []string{"0", "999999", "x"} {
		assert.Equal(t, http.StatusNotFound, s.get(paths.LicenseText(id), "HX-Request", "true").Code, id)
	}
}
