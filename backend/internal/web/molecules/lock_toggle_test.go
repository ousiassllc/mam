package molecules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func TestLockToggle_MarksTheCurrentModeAndPutsToTheURL(t *testing.T) {
	html := render(t, molecules.LockToggle(molecules.LockToggleData{
		DOMID: "lock-s1-camera", URL: "/shots/s1/locks", Parameter: domain.ParamCamera, Mode: domain.LockLocked,
	}))

	assert.Contains(t, html, `id="lock-s1-camera"`)
	assert.Contains(t, html, `hx-put="/shots/s1/locks"`)
	assert.Contains(t, html, `hx-target="this"`)
	assert.Contains(t, html, `<input type="hidden" name="parameter" value="camera">`)
	assert.Contains(t, html, `name="mode" value="lock" aria-pressed="true"`)
	assert.Contains(t, html, `name="mode" value="auto" aria-pressed="false"`)
	assert.Contains(t, html, `name="mode" value="manual" aria-pressed="false"`)
	assert.NotContains(t, html, "<select")
}

func TestLockToggle_OffersTheVideoModelOfManualAndLockInTheSharedFieldLook(t *testing.T) {
	for _, mode := range []domain.LockMode{domain.LockManual, domain.LockLocked} {
		html := render(t, molecules.LockToggle(molecules.LockToggleData{
			DOMID: "lock-s1-video_model", URL: "/shots/s1/locks", Parameter: domain.ParamVideoModel, Mode: mode,
			Models: []domain.ModelName{"kling-3.0", "seedance-2.0"}, Model: "seedance-2.0",
		}))

		assert.Contains(t, html, `aria-label="Video Model"`, mode)
		assert.Contains(t, html, `name="value"`, mode)
		assert.Contains(t, html, "text-neutral-100", mode) // the look of atoms.Select, not a hand-written one
		assert.Contains(t, html, `<option value="kling-3.0">kling-3.0</option>`, mode)
		assert.Contains(t, html, `<option value="seedance-2.0" selected>seedance-2.0</option>`, mode)
		assert.Contains(t, html, `<option value="">（現在のモデル）</option>`, mode)
	}
}

func TestLockToggle_CarriesTheHiddenFieldsAndTargetOfAWorldStateSwitch(t *testing.T) {
	html := render(t, molecules.LockToggle(molecules.LockToggleData{
		DOMID: "lock-wa", URL: "/projects/p1/world-state/locks", Parameter: domain.ParamStory, Mode: domain.LockAuto,
		Label: "Mood", Modes: []domain.LockMode{domain.LockAuto, domain.LockLocked},
		Hidden: []molecules.HiddenField{{Name: "target_type", Value: "scene"}, {Name: "target_id", Value: "sc1"}},
		Target: "#world-state",
	}))

	assert.Contains(t, html, `hx-target="#world-state"`)
	assert.Contains(t, html, `<input type="hidden" name="target_type" value="scene">`)
	assert.Contains(t, html, `<input type="hidden" name="target_id" value="sc1">`)
	assert.Contains(t, html, "Mood")
	assert.Contains(t, html, `value="lock"`)
	assert.NotContains(t, html, `value="manual"`)
}
