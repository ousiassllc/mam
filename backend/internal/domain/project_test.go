package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestCanTransition_FollowsTheCoreStateMachine(t *testing.T) {
	for _, m := range [][2]domain.ProjectState{
		{domain.StateDraft, domain.StatePlanning},
		{domain.StatePlanning, domain.StateAwaitingApproval},
		{domain.StatePlanning, domain.StateVisualDesign},
		{domain.StateAwaitingApproval, domain.StateVisualDesign},
		{domain.StateVisualDesign, domain.StateGenerating},
		{domain.StateJudging, domain.StateRepairing},
		{domain.StateReady, domain.StateExported},
	} {
		assert.True(t, domain.CanTransition(m[0], m[1]), "%s -> %s", m[0], m[1])
	}
	for _, m := range [][2]domain.ProjectState{
		{domain.StateDraft, domain.StateGenerating},
		{domain.StatePlanning, domain.StateGenerating},
		{domain.StateExported, domain.StateDraft},
		{domain.StateReady, domain.StatePlanning},
		{domain.StateDraft, domain.StateDraft},
	} {
		assert.False(t, domain.CanTransition(m[0], m[1]), "%s -> %s", m[0], m[1])
	}
}

func TestCanTransition_EveryCheckpointMoveIsAllowed(t *testing.T) {
	for _, cp := range domain.Checkpoints() {
		assert.True(t, domain.CanTransition(domain.StateAwaitingApproval, cp.ApprovedState()), "%s approved", cp)
		assert.True(t, domain.CanTransition(domain.StateAwaitingApproval, cp.ChangesRequestedState()), "%s changes requested", cp)
	}
}
