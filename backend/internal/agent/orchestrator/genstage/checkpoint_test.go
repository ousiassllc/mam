package genstage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The generation approval is asked at every Confirmation Level, FULL AUTO included, whatever CUSTOM CHECK chose; it is not a choice.
func TestCheckpointGeneration_IsAskedAtEveryLevelAndIsNotSelectable(t *testing.T) {
	for _, level := range []domain.ConfirmationLevel{domain.ConfirmFullAuto, domain.ConfirmKeyCheck, domain.ConfirmCustomCheck} {
		assert.True(t, level.RequiresApproval(domain.CheckpointGeneration, nil), level)
		assert.True(t, level.RequiresApproval(domain.CheckpointGeneration, []domain.Checkpoint{domain.CheckpointFinal}), level)
	}
	assert.False(t, domain.CheckpointGeneration.Selectable())
	assert.NotContains(t, domain.KeyCheckpoints(), domain.CheckpointGeneration)
	assert.True(t, domain.CheckpointGeneration.Valid())
	// Answering leaves the Project in VISUAL_DESIGN either way: the Shots go on one by one, a decline waits.
	assert.Equal(t, domain.StateVisualDesign, domain.CheckpointGeneration.ApprovedState())
	assert.Equal(t, domain.StateVisualDesign, domain.CheckpointGeneration.ChangesRequestedState())
	assert.True(t, domain.CanTransition(domain.StateAwaitingApproval, domain.StateVisualDesign))
	// The other checkpoints are unchanged.
	assert.False(t, domain.ConfirmFullAuto.RequiresApproval(domain.CheckpointScript, nil))
	assert.True(t, domain.ConfirmKeyCheck.RequiresApproval(domain.CheckpointRoughCut, nil))
}
