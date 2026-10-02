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
