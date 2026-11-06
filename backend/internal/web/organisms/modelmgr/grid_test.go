package modelmgr_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ulTag = regexp.MustCompile(`(?s)<ul\b[^>]*>`)

// The number of columns of the cards of the model manager is written in one place, modelcard.GridClass (#1102): no <ul> of a template under
// organisms/modelmgr has a grid-cols- class of its own. The one list that is not a grid of cards is the engine card (mm-engine-list: it holds a form and
// a log and takes the whole width) and the cards of the categories on the overview (mm-categories: not model cards; the overview is as wide as a
// form, so they stay two columns). The <dl> of a card's facts has its own columns and is not a <ul>.
func TestTemplates_OnlyGridClassSetsTheColumnsOfAList(t *testing.T) {
	engineLists := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".templ") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, tag := range ulTag.FindAllString(string(src), -1) {
			if strings.Contains(tag, `data-testid="mm-engine-list"`) {
				engineLists++
				assert.Contains(t, tag, `class="grid grid-cols-1 gap-4"`, "the engine card takes the whole width: %s", path)
				continue
			}
			if strings.Contains(tag, `data-testid="mm-categories"`) {
				continue
			}
			assert.NotContains(t, tag, "grid-cols-", "%s: the columns of a list of cards are modelcard.GridClass's: %s", path, tag)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, engineLists, "the engine card is the one list with its own class")
}
