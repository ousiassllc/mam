package genstage_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// When a job the Gate started changes, the Orchestrator is asked to look at the Project again (the Reference Pack that ended is followed by its
// generation); a job somebody started by hand, or one that is not a generation, asks nothing.
func TestAutoStart_AJobTheGateStartedCallsForAnotherLookAndAManualOneDoesNot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	r.handle(id)
	var kicked []string
	kick := func(_ context.Context, projectID string) { kicked = append(kicked, projectID) }

	r.gate.JobChanged(ctx, r.gen.jobOf[shots[0]], kick)
	assert.Equal(t, []string{id}, kicked, "the job of a Shot the Gate started")

	other := r.planned(domain.ConfirmFullAuto)
	otherShot := r.shotIDs(other)[0]
	payload, _ := json.Marshal(map[string]string{"shot_id": otherShot})
	manual, err := r.jobs.Create(ctx, domain.JobGenerate, payload)
	require.NoError(t, err)
	r.gate.JobChanged(ctx, manual.ID, kick)
	plan, err := r.jobs.Create(ctx, domain.JobPlan, json.RawMessage(`{"project_id":"x"}`))
	require.NoError(t, err)
	r.gate.JobChanged(ctx, plan.ID, kick)
	r.gate.JobChanged(ctx, "no-such-job", kick)
	assert.Len(t, kicked, 1, "a Shot started by hand, a plan job and an unknown job are not followed up")
}
