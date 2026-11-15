package modelpick_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// 動画の種類 (Issue #1014): the panel of a screen that lists video models offers every mode, the ones in force are checked, and each is a chip with an ×.
func TestVideoModes_ThePanelOffersThemAndAChipSaysTheOnesInForce(t *testing.T) {
	f := modelpick.Filter{
		ID: "bar", Action: "/models", Target: "#list", Facets: modelpick.Facets{VideoModes: true},
		Cond: modelfilter.Condition{VideoModes: []traits.VideoMode{traits.ModeFLF2V, traits.ModeR2V}},
	}

	var panel, summary strings.Builder
	require.NoError(t, modelpick.Panel(f, false).Render(context.Background(), &panel))
	require.NoError(t, modelpick.Summary(f).Render(context.Background(), &summary))

	assert.Contains(t, panel.String(), "動画の種類")
	for _, m := range traits.AllVideoModes() {
		assert.Contains(t, panel.String(), `name="video_modes" value="`+string(m)+`"`, "every mode is a choice")
		assert.Contains(t, panel.String(), m.Label())
	}
	assert.Regexp(t, `name="video_modes" value="flf2v"[^>]* checked`, panel.String())
	assert.Regexp(t, `name="video_modes" value="r2v"[^>]* checked`, panel.String())
	assert.NotRegexp(t, `name="video_modes" value="t2v"[^>]* checked`, panel.String())
	assert.Contains(t, summary.String(), "動画の種類: FLF2V（先頭・最後のフレーム）")
	assert.Contains(t, summary.String(), "動画の種類: R2V（参照画像から動画）")
	chips := f.Chips()
	require.Len(t, chips, 2)
	assert.Contains(t, chips[0].Remove, "video_modes=r2v", "removing the first chip keeps the other mode")
	assert.NotContains(t, chips[0].Remove, "flf2v")
	assert.NotContains(t, chips[1].Remove, "r2v")
	assert.Equal(t, 1, f.Active(), "the list is one filter")
}

func TestVideoModes_AScreenWithoutVideoModelsHasNoSection(t *testing.T) {
	var panel strings.Builder
	require.NoError(t, modelpick.Panel(modelpick.Filter{ID: "bar", Facets: modelpick.Facets{Commercial: true}}, false).Render(context.Background(), &panel))

	assert.NotContains(t, panel.String(), `name="video_modes"`)
	assert.NotContains(t, panel.String(), "動画の種類")
}
