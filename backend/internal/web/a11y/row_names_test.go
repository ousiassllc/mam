// Package a11y_test checks, over the presentation packages, what a screen reader announces. It lives
// outside organisms because that directory is over the line limit.
package a11y_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

// buttonNames returns the accessible name of every button whose visible text is label: the aria-label
// when it has one, the text otherwise.
func buttonNames(html, label string) []string {
	var names []string
	for _, m := range regexp.MustCompile(`<button([^>]*)>([^<]*)</button>`).FindAllStringSubmatch(html, -1) {
		if strings.TrimSpace(m[2]) != label {
			continue
		}
		if a := regexp.MustCompile(`aria-label="([^"]*)"`).FindStringSubmatch(m[1]); a != nil {
			names = append(names, a[1])
		} else {
			names = append(names, label)
		}
	}
	return names
}

// The delete button of every constraint has its own name, led by its visible text (#783).
func TestBrandConstraintList_DeleteButtonsAreNamedAfterTheirConstraint(t *testing.T) {
	html := render(t, organisms.BrandConstraintItems(organisms.BrandConstraintListData{
		ProjectID: "p1",
		Constraints: domain.BrandConstraints{
			{ID: "c1", Type: domain.BrandGuideline, Level: domain.EnforceLock, Text: "ロゴを歪めない"},
			{ID: "c2", Type: domain.BrandGuideline, Level: domain.EnforceLock, Text: "競合の色を使わない"},
		},
	}))

	assert.Equal(t, []string{"削除: ロゴを歪めない", "削除: 競合の色を使わない"}, buttonNames(html, "削除"))
}

func TestStoragePanel_ArchiveButtonsAreNamedAfterTheirTake(t *testing.T) {
	html := render(t, organisms.StoragePanel(organisms.StoragePanelData{
		ProjectID: "p1",
		Storage: domain.StorageSummary{Takes: []domain.StoredTake{
			{TakeID: "t1", SceneIndex: 0, ShotIndex: 0, ModelName: "kling", Status: domain.TakeRejected, SizeBytes: 1 << 20, Archivable: true},
			{TakeID: "t2", SceneIndex: 0, ShotIndex: 1, ModelName: "kling", Status: domain.TakeRejected, SizeBytes: 1 << 20, Archivable: true},
		}},
	}))

	names := buttonNames(html, "アーカイブ")
	require.Len(t, names, 2)
	assert.NotEqual(t, names[0], names[1])
	for _, n := range names {
		assert.True(t, strings.HasPrefix(n, "アーカイブ: Scene 1 · Shot "), n)
	}
}
