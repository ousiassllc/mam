package atoms_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

func TestButtonLink_IsAnAnchorWithTheButtonLookAndTheDownloadAttribute(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, atoms.ButtonLink(atoms.ToneNeutral, "ダウンロード", templ.SafeURL("/projects/p1/bundle"), templ.Attributes{"download": true}).Render(context.Background(), &sb))
	out := sb.String()
	assert.Contains(t, out, `<a href="/projects/p1/bundle"`)
	assert.Contains(t, out, " download")
	assert.Contains(t, out, "border-neutral-600")
	assert.Contains(t, out, ">ダウンロード</a>")
	assert.NotContains(t, out, "hx-boost")
}

func TestLinkButton_PrimaryToneIsTheAccentColour(t *testing.T) {
	render := func(tone atoms.ButtonTone) string {
		var sb strings.Builder
		require.NoError(t, atoms.LinkButton(tone, "開く", templ.Attributes{"type": "button"}).Render(context.Background(), &sb))
		return sb.String()
	}
	assert.Contains(t, render(atoms.TonePrimary), "text-indigo-300")
	assert.Contains(t, render(atoms.ToneNeutral), "text-neutral-400")
}
