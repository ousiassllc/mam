package molecules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func TestShotCard_EscapesImportedShotAndTakeIDsInEveryURL(t *testing.T) {
	html := render(t, molecules.ShotCard(molecules.ShotCardData{
		ProjectID: "p",
		Shot:      domain.Shot{ID: "s?1#x", SceneID: "sc", State: domain.ShotGenerating},
		Takes:     []domain.Take{{ID: "t?1%x", ShotID: "s?1#x", Status: domain.TakeAIPick, FilePath: "t.mp4"}},
	}))

	assert.Contains(t, html, `/shots/s%3F1%23x`)
	assert.Contains(t, html, `/ws/shots/s%3F1%23x`)
	assert.Contains(t, html, `/takes/t%3F1%25x/select`)
	assert.Contains(t, html, `/takes/t%3F1%25x`)
	assert.NotContains(t, html, `/shots/s?1`)
	assert.NotContains(t, html, `/takes/t?1`)
	assert.Contains(t, html, `/projects/p/scenes/sc?shot=s%3F1%23x`)
	assert.NotContains(t, html, `?shot=s?1`)
}
