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

// A button repeated down a list names the row it acts on, starting with its visible text (#783).
func TestRowLabel_VisibleLabelFirstThenTheRow(t *testing.T) {
	assert.Equal(t, "削除: 夜の街", atoms.RowLabel("削除", "夜の街"))
	assert.Equal(t, "削除: a b", atoms.RowLabel("削除", " a\n b "), "whitespace and line breaks collapse")
	assert.Equal(t, "削除", atoms.RowLabel("削除", "  "), "a nameless row keeps the plain label")
	long := atoms.RowLabel("削除", strings.Repeat("あ", 100))
	assert.Equal(t, "削除: "+strings.Repeat("あ", 60)+"…", long)
}

func TestRowButtons_AreNamedAfterTheirRowAndKeepTheVisibleText(t *testing.T) {
	render := func(c templ.Component) string {
		var sb strings.Builder
		require.NoError(t, c.Render(context.Background(), &sb))
		return sb.String()
	}
	small := render(atoms.SmallButtonFor(atoms.ToneNeutral, "削除", "制約A", templ.Attributes{"type": "button"}))
	assert.Contains(t, small, `aria-label="削除: 制約A"`)
	assert.Contains(t, small, ">削除</button>")
	link := render(atoms.LinkButtonFor("選択", "Take A", templ.Attributes{"type": "button"}))
	assert.Contains(t, link, `aria-label="選択: Take A"`)
	assert.Contains(t, link, ">選択</button>")
}
