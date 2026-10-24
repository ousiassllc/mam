package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// After an abnormal stop an install is run again.
func TestRecoverResumesAnInstall(t *testing.T) {
	r := newRig(t)
	install, err := r.table.Create(ctx(), domain.JobModelInstall, json.RawMessage(`{"target":"model:m-a"}`))
	require.NoError(t, err)
	require.NoError(t, r.table.MarkRunning(ctx(), install.ID))
	resumed, failed, err := r.queue.Recover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, resumed)
	assert.Zero(t, failed)
	st := r.wait(install.ID)
	assert.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.True(t, r.exists(ckptA), "the interrupted install finished")
}

func TestJobStatusKnowsOnlyItsKinds(t *testing.T) {
	r := newRig(t)
	other, err := r.table.Create(ctx(), domain.JobPlan, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = r.svc.JobStatus(ctx(), other.ID)
	assert.ErrorIs(t, err, modelmanager.ErrNotFound)
	_, err = r.svc.JobStatus(ctx(), "no-such-job")
	assert.ErrorIs(t, err, modelmanager.ErrNotFound)
}
