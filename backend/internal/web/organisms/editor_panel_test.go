package organisms_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

// roughCutButton is the opening tag of the Rough Cut button of the panel.
var roughCutButton = regexp.MustCompile(`<button[^>]*edit/rough-cut[^>]*>`)

// disabledAttr matches the disabled attribute, not the disabled: variants of the class list or hx-disabled-elt.
var disabledAttr = regexp.MustCompile(`\sdisabled[\s=>]`)

// The Rough Cut button can be pressed only in the Project states the server accepts a Rough Cut in
// (#337): from the others it answers 409, after the confirmation dialog of a re-make.
func TestEditorPanel_RoughCutButtonFollowsTheStatesTheServerEditsIn(t *testing.T) {
	for _, state := range domain.ProjectStates() {
		for _, timelineID := range []string{"", "tl1"} {
			var sb strings.Builder
			require.NoError(t, organisms.EditorActions(organisms.EditorPanelData{
				Project:    domain.Project{ID: "p", State: state},
				TimelineID: timelineID,
			}).Render(context.Background(), &sb))
			button := roughCutButton.FindString(sb.String())
			require.NotEmpty(t, button, "state %s timeline %q", state, timelineID)
			assert.Equal(t, !state.CanEdit(), disabledAttr.MatchString(button), "state %s timeline %q: %s", state, timelineID, button)
		}
	}
}

// exportButton is the opening tag of the export button of the panel.
var exportButton = regexp.MustCompile(`<button[^>]*type="submit"[^>]*>\s*書き出す`)

func renderPanelActions(t *testing.T, d organisms.EditorPanelData) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, organisms.EditorActions(d).Render(context.Background(), &sb))
	return sb.String()
}

// The export button can be pressed only when the server accepts the export (#339): the Project is
// READY / EXPORTED and the series shown has audio made for its latest Edit Timeline. A Branch without
// audio (404) or with audio of an older edit (409) gets a disabled button and says why.
func TestEditorPanel_ExportButtonFollowsWhatTheServerExports(t *testing.T) {
	current := &domain.AudioTimelineVersion{Timeline: domain.AudioTimeline{EditTimelineID: "tl2"}}
	stale := &domain.AudioTimelineVersion{Timeline: domain.AudioTimeline{EditTimelineID: "tl1"}}
	cases := []struct {
		name    string
		state   domain.ProjectState
		audio   *domain.AudioTimelineVersion
		render  bool
		enabled bool
		blocker string
	}{
		{"current audio", domain.StateReady, current, true, true, ""},
		{"exported again", domain.StateExported, current, true, true, ""},
		{"no audio", domain.StateReady, nil, true, false, "音声がない"},
		{"audio of an older edit", domain.StateReady, stale, true, false, "最新の編集に対応していない"},
		{"not exportable state", domain.StateAudio, current, true, false, ""},
		{"no ffmpeg", domain.StateReady, current, false, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := renderPanelActions(t, organisms.EditorPanelData{
				Project: domain.Project{ID: "p", State: tc.state}, BranchID: "b1", TimelineID: "tl2", Audio: tc.audio, CanRender: tc.render,
			})
			button := exportButton.FindString(body)
			require.NotEmpty(t, button)
			assert.Equal(t, !tc.enabled, disabledAttr.MatchString(button), button)
			if tc.blocker == "" {
				assert.NotContains(t, body, "data-export-blocked")
			} else {
				assert.Contains(t, body, "data-export-blocked")
				assert.Contains(t, body, tc.blocker)
			}
		})
	}
}
