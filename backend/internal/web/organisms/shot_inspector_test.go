package organisms_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

// The Inspector draws a form only in the Shot states in which the server accepts it (#334): the spec
// save and the version restore are refused (409) while the Shot is generated or repaired, the Reference
// Pack add while it is generated, repaired or judged.
func TestShotInspector_LeavesOutTheFormsTheServerRefusesInTheShotState(t *testing.T) {
	now := time.Now()
	for _, state := range domain.ShotStates() {
		var sb strings.Builder
		require.NoError(t, organisms.ShotInspector(organisms.ShotInspectorData{
			Project: domain.Project{ID: "p"},
			Shot:    domain.Shot{ID: "s1", SceneID: "sc", State: state},
			Library: []domain.ReferenceItem{{ID: "r1", Name: "ref"}},
			Versions: []domain.VersionEntry{
				{Number: 2, CreatedAt: now},
				{Number: 1, CreatedAt: now},
			},
		}).Render(context.Background(), &sb))
		html := sb.String()

		specBlocked := state == domain.ShotGenerating || state == domain.ShotRepairing
		pinBlocked := state != domain.ShotPlanning && state != domain.ShotEditing
		assert.Equal(t, !specBlocked, strings.Contains(html, `hx-put="/shots/s1/shot-spec"`), "spec form in %s", state)
		assert.Equal(t, !specBlocked, strings.Contains(html, `/shots/s1/versions/1/restore`), "restore button in %s", state)
		assert.Equal(t, !pinBlocked, strings.Contains(html, `hx-post="/shots/s1/references"`), "reference form in %s", state)
		assert.Equal(t, specBlocked, strings.Contains(html, "data-spec-edit-blocked"), "spec notice in %s", state)
		assert.Equal(t, pinBlocked, strings.Contains(html, "data-pin-reference-blocked"), "reference notice in %s", state)
	}
}
