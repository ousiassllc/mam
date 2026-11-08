package genstage_test

import (
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
