package modelcards_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
)

// 動画の種類 (Issue #1014): the video picker of the generation test offers the group (an image picker has no use for it), and the card of an installed local video
// model carries its kinds as badges.
func TestVideoModesAreOfferedByTheVideoPickerOnly(t *testing.T) {
	assert.True(t, modelcards.Facets("video", nil).VideoModes)
	assert.False(t, modelcards.Facets("image", nil).VideoModes)
}

func TestTheCardOfALocalVideoModelShowsItsVideoModes(t *testing.T) {
	local := modelcatalog.Info{ID: "comfyui:bundled-comfyui/wan21-vace-1-3b-t2v", Media: "video", Origin: "local", Catalog: &modelcatalog.CatalogTraits{
		Traits: traits.Traits{VideoModes: []traits.VideoMode{traits.ModeT2V, traits.ModeVACE, traits.ModeR2V}},
	}}
	cloud := modelcatalog.Info{ID: "runway-gen-4.5", Media: "video", Origin: "api"}

	var labels []string
	for _, b := range modelcards.FromInfo(local).Badges {
		if b.Attrs["data-testid"] == modelcard.TestIDVideoMode {
			labels = append(labels, b.Label)
		}
	}
	assert.Equal(t, []string{"T2V", "VACE", "R2V"}, labels)
	for _, b := range modelcards.FromInfo(cloud).Badges {
		assert.NotEqual(t, modelcard.TestIDVideoMode, b.Attrs["data-testid"], "a cloud model has no kind in the catalog")
	}
}
