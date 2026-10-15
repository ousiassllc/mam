package worldstate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/worldstate"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

func data() worldstate.Data {
	return worldstate.Data{
		Project: domain.Project{ID: "p1"},
		World:   domain.WorldContext{Version: 2, Characters: []domain.WorldCharacter{{ID: "c1", Name: "Mio"}, {ID: "c2", Name: "Ren"}}},
		Locks:   domain.LockSet{{Target: domain.TargetCharacter, ID: "c1", Field: "wardrobe"}: {}},
	}
}

// One operation answers with the part it changed: a response of the whole view would replace the other
// forms and discard what the user typed in them (#685).
func TestLockSwitched_AnswersTheSwitchAndTheInputOfItsEntityOnly(t *testing.T) {
	html := render(t, worldstate.LockSwitched(data(), domain.TargetCharacter, "c1", "wardrobe"))

	assert.Contains(t, html, `id="lock-character-c1-wardrobe"`)
	assert.Contains(t, html, `id="ws-character-c1-wardrobe-field" hx-swap-oob="true"`)
	assert.Contains(t, html, `id="ws-save-character-c1" hx-swap-oob="true"`)
	assert.Contains(t, html, "disabled", "the input of the LOCKed attribute is disabled now")
	for _, other := range []string{`id="world-state-view"`, `id="ws-form-character-c1"`, `id="ws-form-character-c2"`, `name="hairstyle"`, "<details"} {
		assert.NotContains(t, html, other, "the other inputs keep what was typed, the LOCK settings stay open")
	}
}

func TestLockSwitched_ARefusedCameraSwitchRedrawsOnlyTheCameraValues(t *testing.T) {
	html := render(t, worldstate.LockSwitched(data(), domain.TargetCamera, "", "lens"))

	assert.Contains(t, html, `id="world-state-camera"`)
	assert.Contains(t, html, `hx-swap-oob="true"`)
	assert.NotContains(t, html, `id="ws-form-character-c1"`)
}

func TestEntitySaved_AnswersTheFormOfThatEntityWithTheVersionOutOfBand(t *testing.T) {
	html := render(t, worldstate.EntitySaved(data(), domain.TargetCharacter, "c2"))

	assert.Contains(t, html, `id="ws-form-character-c2"`)
	assert.Contains(t, html, `id="world-state-version"`)
	assert.Contains(t, html, `id="version-history-world"`)
	assert.Equal(t, 2, strings.Count(html, `hx-swap-oob="true"`))
	for _, other := range []string{`id="world-state-view"`, `id="ws-form-character-c1"`, "<details"} {
		assert.NotContains(t, html, other)
	}
}

func TestReferenceAssigned_AnswersTheFormOfThatSlotOnly(t *testing.T) {
	d := data()
	d.Library = []domain.ReferenceItem{{ID: "r1", AssetID: "a1", Kind: domain.RefCharacter, Name: "Mio face"}}
	html := render(t, worldstate.ReferenceAssigned(d, domain.TargetCharacter, "c1", "face"))

	assert.Contains(t, html, `id="ws-slot-form-character-c1-face"`)
	for _, other := range []string{`id="ws-slot-form-character-c1-body"`, `id="ws-form-character-c1"`, `id="world-state-view"`} {
		assert.NotContains(t, html, other)
	}
}

func TestView_CarriesEveryPartUnderTheIdItsAnswerReplaces(t *testing.T) {
	html := render(t, worldstate.View(data()))

	for _, id := range []string{"world-state-view", "world-state-version", "world-state-camera", "world-state-story", "version-history-world", "ws-form-character-c1", "ws-save-character-c2", "ws-character-c1-wardrobe-field", "ws-slot-form-character-c1-face"} {
		assert.Contains(t, html, `id="`+id+`"`)
	}
	assert.NotContains(t, html, "hx-swap-oob", "the view itself is not an out-of-band answer")
}
