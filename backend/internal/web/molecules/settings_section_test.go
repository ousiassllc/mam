package molecules_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

// What the section is for is not on the page: it is the content of the help behind the "?" beside the heading, and a
// section without one has no button.
func TestSettingsSection_ShowsItsExplanationOnlyInTheHelpBesideTheHeading(t *testing.T) {
	render := func(title, explanation string) string {
		var sb strings.Builder
		content := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, "<p>CONTENT</p>")
			return err
		})
		ctx := templ.WithChildren(context.Background(), content)
		require.NoError(t, molecules.SettingsSection(title, explanation).Render(ctx, &sb))
		return sb.String()
	}

	with := render("予算", "説明文")
	assert.Contains(t, with, "<h2")
	assert.Less(t, strings.Index(with, "予算"), strings.Index(with, "data-help-toggle"))
	assert.Contains(t, with, `aria-label="予算の説明"`)
	assert.Less(t, strings.Index(with, "data-help-panel hidden"), strings.Index(with, "説明文"), "the text is inside the hidden panel")
	assert.Less(t, strings.Index(with, "説明文"), strings.Index(with, "CONTENT"))
	assert.Equal(t, 1, strings.Count(with, "説明文"))

	without := render("予算", "")
	assert.Contains(t, without, "CONTENT")
	assert.NotContains(t, without, "data-help", "no button that opens nothing")
}

func TestSettingsSectionH3_IsTheSameUnderAnH3(t *testing.T) {
	var sb strings.Builder
	ctx := templ.WithChildren(context.Background(), templ.Raw("<p>CONTENT</p>"))
	require.NoError(t, molecules.SettingsSectionH3("API キー", "説明").Render(ctx, &sb))

	assert.Contains(t, sb.String(), `<h3 class="text-base font-semibold">API キー</h3>`)
	assert.NotContains(t, sb.String(), "<h2")
	assert.Contains(t, sb.String(), `aria-label="API キーの説明"`)
}

func TestSettingsSectionAt_DrawsTheHeadingAtTheLevelOrNoneAtAll(t *testing.T) {
	render := func(level molecules.HeadingLevel, description string) string {
		var sb strings.Builder
		content := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, "<p>CONTENT</p>")
			return err
		})
		ctx := templ.WithChildren(context.Background(), content)
		require.NoError(t, molecules.SettingsSectionAt(level, "区画", description).Render(ctx, &sb))
		return sb.String()
	}

	assert.Contains(t, render(molecules.HeadingH3, ""), `<h3 class="text-base font-semibold">区画</h3>`)
	assert.NotContains(t, render(molecules.HeadingH3, ""), "<h2")

	none := render(molecules.HeadingNone, "説明文")
	assert.NotContains(t, none, "区画", "the title would repeat the h2 of the category")
	assert.NotContains(t, none, "<h")
	assert.Contains(t, none, "説明文")
	assert.Contains(t, none, "CONTENT")

	bare := render(molecules.HeadingNone, "")
	assert.NotContains(t, bare, "text-neutral-400")
	assert.Contains(t, bare, "CONTENT")
}
