package help_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/help"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

// The button is named by what it explains (so that the "?" of a page are told apart in a list of buttons), says it is closed,
// opens the popover with the same id (popovertarget: it works from a click, Enter and Space) and names it; the icon is
// decoration.
func TestToggle_IsANamedButtonThatControlsItsPanel(t *testing.T) {
	html := render(t, help.Toggle("help-x", "API キー"))

	assert.Contains(t, html, `<button type="button" data-help-toggle popovertarget="help-x" aria-expanded="false" aria-controls="help-x" aria-label="API キーの説明"`)
	assert.Contains(t, html, `aria-hidden="true"`, "the icon is decoration")
	assert.NotContains(t, html, "text-neutral-500")
}

// The panel is a native popover (closed until opened, in the top layer, Esc and an outside click close it) that is not a
// dialog: a group named like its button. It has no display class, which would show it while it is closed.
func TestPanel_IsAnUnclassedPopoverNamedLikeItsButtonAndHoldsItsChildren(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<p>説明</p>"))
	var sb strings.Builder
	require.NoError(t, help.Panel("help-x", "API キー").Render(ctx, &sb))

	assert.Contains(t, sb.String(), `id="help-x" popover role="group" aria-label="API キーの説明" data-help-panel`)
	assert.NotRegexp(t, `class="[^"]*\b(flex|grid|block|inline|hidden)\b`, sb.String(), "a display class would show the closed popover")
	assert.NotContains(t, sb.String(), " hidden")
	assert.Contains(t, sb.String(), "<p>説明</p>")
	assert.NotContains(t, sb.String(), "text-neutral-500")
}

// A row with a help is the heading, its button and the popover after it (so that Tab goes from the button into it); the
// button and the popover name each other.
func TestRow_PutsTheToggleBesideTheHeadingAndTheExplanationInAPopover(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<h2>見出し</h2>"))
	var sb strings.Builder
	require.NoError(t, help.Row(help.ID("見出し"), "見出し", help.Text("一行目\n二行目")).Render(ctx, &sb))
	html := sb.String()

	id := regexp.MustCompile(`aria-controls="([^"]+)"`).FindStringSubmatch(html)
	require.NotNil(t, id)
	assert.Contains(t, html, `id="`+id[1]+`" popover`)
	assert.Less(t, strings.Index(html, "<h2>見出し</h2>"), strings.Index(html, "data-help-toggle"))
	assert.Less(t, strings.Index(html, "data-help-toggle"), strings.Index(html, "data-help-panel"))
	assert.Equal(t, 2, strings.Count(html, "<p "), "one paragraph per line")
	assert.Contains(t, html, "二行目")
}

// Without an explanation the heading is alone: no button that opens nothing.
func TestRow_WithoutAnExplanationIsTheHeadingAlone(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<h2>見出し</h2>"))
	var sb strings.Builder
	require.NoError(t, help.Row("help-x", "見出し", nil).Render(ctx, &sb))

	assert.Equal(t, "<h2>見出し</h2>", sb.String())
}

// The id comes from the subject alone: a render again (a swap) keeps it, another subject has another one.
func TestID_IsStableAndDiffersBySubject(t *testing.T) {
	assert.Equal(t, help.ID("API キー"), help.ID("API キー"))
	assert.NotEqual(t, help.ID("API キー"), help.ID("ChatGPT でログイン"))
	assert.Regexp(t, `^help-[0-9a-f]+$`, help.ID("x"))
}
