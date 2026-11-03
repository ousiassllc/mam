package a11y_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/jobs"
)

// The link and the progress bar of every job name their row (kind, subject, start), led by the visible text, so that the links of a list
// are told apart by a link list or a voice command (#793), and the state is a word and not only a colour.
func TestJobsList_LinksAndBarsAreNamedAfterTheirRow(t *testing.T) {
	progress := &jobs.Progress{Percent: 40, Text: "a.safetensors"}
	html := render(t, jobs.Section(jobs.List{Refresh: "/jobs/list", UpdatedAt: "10:00:00", Rows: []jobs.Row{
		{Label: "導入", Subject: "Flux", StateText: "実行中", Tone: atoms.BadgeInfo, StartedAt: "2026-11-03 10:00", Progress: progress, Href: "/model-manager"},
		{Label: "導入", Subject: "Qwen", StateText: "失敗", Tone: atoms.BadgeError, StartedAt: "2026-11-03 09:00", Error: "空きが足りません", Href: "/model-manager"},
	}}))

	links := regexp.MustCompile(`<a [^>]*aria-label="([^"]*)"[^>]*>開く</a>`).FindAllStringSubmatch(html, -1)
	require.Len(t, links, 2)
	assert.Equal(t, "開く: 導入 Flux（2026-11-03 10:00）", links[0][1])
	assert.Equal(t, "開く: 導入 Qwen（2026-11-03 09:00）", links[1][1])
	assert.Contains(t, html, `<progress class="h-2 w-full" max="100" value="40" aria-label="導入 Flux（2026-11-03 10:00）の進み具合">`)
	assert.Contains(t, html, ">実行中</span>")
	assert.Contains(t, html, ">失敗</span>")
}

// The list is swapped every 2 seconds while a job runs, and htmx puts the focus back only on an element with the same id: the "開く" link
// of a row that has a job id is named after it, and a row without one has no id rather than the same id twice (#980).
func TestJobsList_OpenLinkHasAStableID(t *testing.T) {
	html := render(t, jobs.Section(jobs.List{Refresh: "/jobs/list", UpdatedAt: "10:00:00", Rows: []jobs.Row{
		{ID: "j1", Label: "導入", StateText: "実行中", Tone: atoms.BadgeInfo, StartedAt: "2026-11-03 10:00", Href: "/model-manager"},
		{Label: "導入", StateText: "失敗", Tone: atoms.BadgeError, StartedAt: "2026-11-03 09:00", Href: "/model-manager"},
	}}))

	assert.Equal(t, 1, strings.Count(html, ` id="jobs-open-`))
	assert.Contains(t, html, `<a id="jobs-open-j1" href="/model-manager"`)
}
