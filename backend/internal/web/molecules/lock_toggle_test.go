package molecules_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestLockToggle_CarriesTheHiddenFieldsOfAWorldStateSwitchAndReplacesOnlyItself(t *testing.T) {
	html := render(t, molecules.LockToggle(molecules.LockToggleData{
		DOMID: "lock-wa", URL: "/projects/p1/world-state/locks", Parameter: domain.ParamStory, Mode: domain.LockAuto,
		Label: "Mood", Modes: []domain.LockMode{domain.LockAuto, domain.LockLocked},
		Hidden: []molecules.HiddenField{{Name: "target_type", Value: "scene"}, {Name: "target_id", Value: "sc1"}},
	}))

	assert.Contains(t, html, `hx-target="this"`, "only the switch is replaced, never the view around it")
	assert.Contains(t, html, `<input type="hidden" name="target_type" value="scene">`)
	assert.Contains(t, html, `<input type="hidden" name="target_id" value="sc1">`)
	assert.Contains(t, html, "Mood")
	assert.Contains(t, html, `value="lock"`)
	assert.NotContains(t, html, `value="manual"`)
}

// The current mode is not only aria-pressed: it is the one that looks set (a tinted face; LOCK is amber, the others indigo),
// and the modes that are not current look like plain buttons. The page then shows what is stored in both the tree and the
// eye (color-rules.md: the selection is indigo, a warning is amber).
func TestLockToggle_OnlyTheCurrentModeLooksSet(t *testing.T) {
	for mode, tint := range map[domain.LockMode]string{
		domain.LockAuto:   "bg-indigo-500/20 text-indigo-300",
		domain.LockManual: "bg-indigo-500/20 text-indigo-300",
		domain.LockLocked: "bg-amber-500/20 text-amber-300",
	} {
		html := render(t, molecules.LockToggle(molecules.LockToggleData{
			DOMID: "lock-s1-camera", URL: "/shots/s1/locks", Parameter: domain.ParamCamera, Mode: mode,
		}))

		buttons := lockButton.FindAllStringSubmatch(html, -1)
		require.Len(t, buttons, 3, mode)
		for _, b := range buttons {
			value, pressed, class := b[1], b[2], b[3]
			if value == string(mode) {
				assert.Equal(t, "true", pressed, "%s: %s is pressed", mode, value)
				assert.Contains(t, class, tint, "%s: the current mode is tinted", mode)
				assert.NotContains(t, class, "hover:bg-neutral-800", "%s: the current mode is not a plain button", mode)
				continue
			}
			assert.Equal(t, "false", pressed, "%s: %s is not pressed", mode, value)
			assert.Contains(t, class, "text-neutral-300 hover:bg-neutral-800", "%s: %s is a plain button", mode, value)
			assert.NotContains(t, class, "bg-amber-500/20", "%s: %s is not tinted", mode, value)
			assert.NotContains(t, class, "bg-indigo-500/20", "%s: %s is not tinted", mode, value)
		}
	}
}

// lockButton captures the value, aria-pressed and class of each mode button of a LockToggle.
var lockButton = regexp.MustCompile(`name="mode" value="(\w+)" aria-pressed="(\w+)" class="([^"]*)"`)
