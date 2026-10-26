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

func TestSettingsSection_WrapsItsContentUnderTheTitleAndOnlyShowsADescriptionWhenGiven(t *testing.T) {
	render := func(title, description string) string {
		var sb strings.Builder
		content := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, "<p>CONTENT</p>")
			return err
		})
		ctx := templ.WithChildren(context.Background(), content)
		require.NoError(t, molecules.SettingsSection(title, description).Render(ctx, &sb))
		return sb.String()
	}

	with := render("予算", "説明文")
	assert.Contains(t, with, "<h2")
	assert.Contains(t, with, "説明文")
	assert.Less(t, strings.Index(with, "予算"), strings.Index(with, "CONTENT"))

	without := render("予算", "")
	assert.Contains(t, without, "CONTENT")
	assert.NotContains(t, without, "text-neutral-400", "no description paragraph")
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
