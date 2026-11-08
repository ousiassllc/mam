package molecules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

// Choosing AUTO is the consent to start the paid generation (spec/spec.md §4.3), so the mode control says so while AUTO is chosen: that it starts by itself,
// that it costs money, that an unknown price does not stop it, and what stops it. The same words are what the thread and set_project_mode say.
func TestModeControl_AutoSaysItStartsTheGenerationByItself(t *testing.T) {
	html := render(t, molecules.ModeControl(domain.Project{ID: "p1", Mode: domain.ModeAuto}))

	assert.Contains(t, html, `data-testid="auto-start-notice"`)
	assert.Contains(t, html, mainline.AutoStartNotice)
	assert.Contains(t, mainline.AutoStartNotice, "費用がかかります")
	assert.Contains(t, mainline.AutoStartNotice, "不明")
	assert.Contains(t, mainline.AutoStartNotice, "予算上限")
	assert.Contains(t, mainline.AutoStartNotice, "DIRECTOR")
}

// DIRECTOR starts nothing by itself, so it carries no such notice.
func TestModeControl_DirectorHasNoAutoStartNotice(t *testing.T) {
	html := render(t, molecules.ModeControl(domain.Project{ID: "p1", Mode: domain.ModeDirector}))

	assert.NotContains(t, html, `data-testid="auto-start-notice"`)
}

// Whatever the Confirmation Level is, AUTO asks nothing before the generation starts: the level control says so (it decides only the other checkpoints).
func TestConfirmationControl_AutoSaysNoLevelAsksBeforeTheGenerationStarts(t *testing.T) {
	for _, level := range []domain.ConfirmationLevel{domain.ConfirmFullAuto, domain.ConfirmKeyCheck, domain.ConfirmCustomCheck} {
		html := render(t, molecules.ConfirmationControl(domain.Project{ID: "p1", Mode: domain.ModeAuto, ConfirmationLevel: level}))
		assert.Contains(t, html, mainline.AutoStartAtAnyLevel, level)
	}
	html := render(t, molecules.ConfirmationControl(domain.Project{ID: "p1", Mode: domain.ModeDirector, ConfirmationLevel: domain.ConfirmKeyCheck}))
	assert.NotContains(t, html, `data-testid="auto-start-level-notice"`)
}
