package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/mmcat"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// The pages of the categories that have a list of cards are max-w-5xl (three columns need it: at max-w-4xl a card would be about 266 px wide at 1280 px);
// the engine, the face-check models and the overview, which have no such list, stay max-w-4xl (#1102).
func TestModelManagerPages_AreWideOnlyWhereThereIsAListOfCards(t *testing.T) {
	wide := map[string]bool{mmcat.ImageVideo: true, mmcat.CustomNodes: true, mmcat.LocalRuntimes: true, mmcat.HFExternal: true}
	for _, c := range mmcat.All() {
		t.Run(c.Slug, func(t *testing.T) {
			var sb strings.Builder
			require.NoError(t, pages.ModelManagerCategoryPage("tok", c, mmcat.All(), modelmgr.Data{}).Render(context.Background(), &sb))
			if wide[c.Slug] {
				assert.Contains(t, sb.String(), "p-4 md:p-8 max-w-5xl")
			} else {
				assert.Contains(t, sb.String(), "p-4 md:p-8 max-w-4xl")
			}
		})
	}
	var sb strings.Builder
	require.NoError(t, pages.ModelManagerPage("tok", modelmgr.Overview{}).Render(context.Background(), &sb))
	assert.Contains(t, sb.String(), "p-4 md:p-8 max-w-4xl", "the overview has no list of cards")
}
