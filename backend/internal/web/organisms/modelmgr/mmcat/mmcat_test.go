package mmcat_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
)

// All is the one list: the six categories of Issue #1101 in their order, each with a name, a description, an icon and its sections; a slug and a section are in one category only.
func TestAllIsTheOneListOfTheCategories(t *testing.T) {
	var slugs []string
	slugSeen, sectionSeen := map[string]bool{}, map[string]string{}
	for _, c := range mmcat.All() {
		slugs = append(slugs, c.Slug)
		assert.False(t, slugSeen[c.Slug], "slug %s once", c.Slug)
		slugSeen[c.Slug] = true
		assert.Equal(t, "/model-manager/"+c.Slug, c.Href())
		assert.NotEmpty(t, c.Name)
		assert.NotEmpty(t, c.Description)
		assert.NotEmpty(t, c.Icon)
		require.NotEmpty(t, c.Sections)
		for _, id := range c.Sections {
			assert.Empty(t, sectionSeen[id], "#%s is in %s and in %s", id, sectionSeen[id], c.Slug)
			sectionSeen[id] = c.Slug
		}
	}
	assert.Equal(t, []string{"comfyui", "image-video", "custom-nodes", "local-runtimes", "face-check", "hf-external", "companion"}, slugs)
}

func TestFindIsTheCategoryOfASlug(t *testing.T) {
	c, ok := mmcat.Find(mmcat.FaceCheck)
	require.True(t, ok)
	assert.Equal(t, "顔チェック用モデル", c.Name)
	_, ok = mmcat.Find("engine")
	assert.False(t, ok, "the word of a part of the screen is no category")
}
