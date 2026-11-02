package servicetest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// The progress of a batch's ComfyUI calls is found by the ID of its queue job (BatchStatus.JobID, get_generation_test): the job
// Register installs runs its model calls with the job as owner.
func TestTheModelCallsOfABatchAreOwnedByItsJob(t *testing.T) {
	r := gentesttest.New(t)
	ref, err := r.Confirmed(gentest.Request{Media: "image", Models: []string{"flux-2"}, Count: 2, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}})
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, ref.JobID, st.JobID)

	calls := r.Fake.Calls()
	require.Len(t, calls, 2)
	for _, c := range calls {
		assert.Equal(t, ref.JobID, c.Owner)
	}
	assert.NotEqual(t, ref.BatchID, ref.JobID)
}
