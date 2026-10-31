package gatedtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/hfguide"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

// A download that Hugging Face refuses after the token was cleared is told as "no token" (A), not as a token that is not accepted (B).
func TestAGatedDownloadRefusedAfterTheTokenWasClearedIsTheMissingTokenGuidance(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.Svc.SetHuggingFaceToken(r.HF.Token))
	r.HF.Granted.Store(true)
	ref, msg := install(t, r) // the queue is not running yet: the job waits
	require.Empty(t, msg)
	require.NoError(t, r.Svc.ClearHuggingFaceToken())
	st := r.Wait(ref.JobID)
	require.Equal(t, domain.JobFailed, st.State)
	assert.Equal(t, hfguide.NoToken, st.Error)
	assert.NoFileExists(t, r.ModelFile(mmtest.GatedFile))
}
