package mainline_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The generation checkpoint is the legacy of v0.1.34: it is never asked, at any level, and CUSTOM CHECK cannot choose it. The others follow their level.
func TestCheckpointGeneration_IsNeverAskedAndIsNotSelectable(t *testing.T) {
	for _, level := range []domain.ConfirmationLevel{domain.ConfirmFullAuto, domain.ConfirmKeyCheck, domain.ConfirmCustomCheck} {
		assert.False(t, level.RequiresApproval(domain.CheckpointGeneration, nil), level)
		assert.False(t, level.RequiresApproval(domain.CheckpointGeneration, []domain.Checkpoint{domain.CheckpointGeneration, domain.CheckpointFinal}), level)
	}
	assert.False(t, domain.CheckpointGeneration.Selectable())
	assert.NotContains(t, domain.KeyCheckpoints(), domain.CheckpointGeneration)
	assert.True(t, domain.CheckpointGeneration.Valid(), "the cards of v0.1.34 are still read")
	assert.Equal(t, domain.CheckpointGeneration, func() domain.Checkpoint {
		cp, _ := domain.CheckpointForRefType(domain.CheckpointGeneration.RefType())
		return cp
	}())
	assert.Equal(t, domain.StateVisualDesign, domain.CheckpointGeneration.ApprovedState(), "answering a legacy card returns the Project to VISUAL_DESIGN")
	assert.Equal(t, domain.StateVisualDesign, domain.CheckpointGeneration.ChangesRequestedState())
	assert.False(t, domain.ConfirmFullAuto.RequiresApproval(domain.CheckpointScript, nil))
	assert.True(t, domain.ConfirmKeyCheck.RequiresApproval(domain.CheckpointRoughCut, nil))
}

// TestCheckpointTable_FollowsTheSpecTable pins the table of spec/spec.md §4.4 (level × script / generation / rough_cut / final): the generation checkpoint is
// never asked, whatever the level or the CUSTOM CHECK selection (#1111); the other three follow the level.
func TestCheckpointTable_FollowsTheSpecTable(t *testing.T) {
	script, generation, roughCut, final := domain.CheckpointScript, domain.CheckpointGeneration, domain.CheckpointRoughCut, domain.CheckpointFinal
	tests := []struct {
		name   string
		level  domain.ConfirmationLevel
		custom []domain.Checkpoint
		asked  []domain.Checkpoint
	}{
		{"full_auto", domain.ConfirmFullAuto, nil, nil},
		{"full_auto ignores a stored selection", domain.ConfirmFullAuto, []domain.Checkpoint{script, final}, nil},
		{"key_check", domain.ConfirmKeyCheck, nil, []domain.Checkpoint{script, roughCut, final}},
		{"custom_check script only", domain.ConfirmCustomCheck, []domain.Checkpoint{script}, []domain.Checkpoint{script}},
		{"custom_check rough_cut only", domain.ConfirmCustomCheck, []domain.Checkpoint{roughCut}, []domain.Checkpoint{roughCut}},
		{"custom_check final only", domain.ConfirmCustomCheck, []domain.Checkpoint{final}, []domain.Checkpoint{final}},
		{"custom_check naming generation does not ask it", domain.ConfirmCustomCheck, []domain.Checkpoint{generation, final}, []domain.Checkpoint{final}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, cp := range []domain.Checkpoint{script, generation, roughCut, final} {
				assert.Equal(t, slices.Contains(tt.asked, cp), tt.level.RequiresApproval(cp, tt.custom), cp)
			}
		})
	}
}
