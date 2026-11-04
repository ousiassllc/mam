package paths_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The links of the error box (apperr cannot import the paths: it is below them) go to screens that exist. This pins every place of every code
// to the path of the screen.
func TestErrorLinksGoToTheScreensThatExist(t *testing.T) {
	screens := map[string]bool{
		paths.Settings(): true,
		paths.SettingsCategory(paths.SettingsAPIKeys):     true,
		paths.SettingsCategory(paths.SettingsLLM):         true,
		paths.SettingsCategory(paths.SettingsLocalModels): true,
		paths.SettingsCategory(paths.SettingsFFmpeg):      true,
		paths.SettingsCategory(paths.SettingsFaceFusion):  true,
		paths.SettingsCategory(paths.SettingsUpdate):      true,
		paths.ModelManager():                              true,
		paths.ChatGPTSettings():                           true,
	}
	for _, c := range apperr.Codes() {
		for _, l := range apperr.New(c).View(apperr.LangJa).Settings {
			assert.True(t, screens[l.Path], "%s links to %s", c, l.Path)
		}
	}
}
