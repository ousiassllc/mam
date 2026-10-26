package shotgen_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotgen"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shottest"
)

func register(env *mcptest.Env, r *mcpkit.Registrar) {
	jobs.Register(r, env.Services)
	shotgen.Register(r, env.Services)
}

func newFixture(t *testing.T, o shottest.Options) *shottest.Fixture {
	t.Helper()
	return shottest.New(t, register, o)
}

// waitJob polls get_job until the job reaches a final state.
func waitJob(t *testing.T, f *shottest.Fixture, id string) mcpkit.JobStatus {
	t.Helper()
	var st mcpkit.JobStatus
	require.Eventually(t, func() bool {
		f.CallOK("get_job", map[string]any{"job_id": id}, &st)
		return st.State.Final()
	}, 30*time.Second, 10*time.Millisecond, "job %s never finished (last: %+v)", id, st)
	return st
}

func TestGenerateShot_ReturnsAJobWhoseStateAndTakesGetJobReports(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	release := f.Video.Hold()

	var out shotgen.ShotJobOutput
	f.CallOK("generate_shot", map[string]any{"shot_id": f.ShotID}, &out)
	require.NotEmpty(t, out.JobID)
	assert.Equal(t, f.ShotID, out.ShotID)
	assert.Equal(t, "GENERATING", out.ShotState)

	var st mcpkit.JobStatus
	f.CallOK("get_job", map[string]any{"job_id": out.JobID}, &st)
	assert.Contains(t, []mcpkit.JobState{mcpkit.JobQueued, mcpkit.JobRunning}, st.State, "the model is held: the job is not over")

	release()
	st = waitJob(t, f, out.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.NotEmpty(t, st.Result["take_ids"], "the Takes the job made")
}

func TestGenerateShot_ValidationBusyAndUnavailableMapToToolErrors(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	assert.Contains(t, f.CallError("generate_shot", map[string]any{"shot_id": "missing"}), "not_found")
	long := make([]byte, 3000)
	for i := range long {
		long[i] = 'a'
	}
	assert.Contains(t, f.CallError("generate_shot", map[string]any{"shot_id": f.ShotID, "additional_instruction": string(long)}), "validation")

	release := f.Video.Hold()
	defer release()
	f.CallOK("generate_shot", map[string]any{"shot_id": f.ShotID}, nil)
	assert.Contains(t, f.CallError("generate_shot", map[string]any{"shot_id": f.ShotID}), "conflict", "a Shot that is being generated")
	release()
	f.WaitIdle(t)

	bare := newFixture(t, shottest.Options{NoModels: true})
	assert.Contains(t, bare.CallError("generate_shot", map[string]any{"shot_id": bare.ShotID}), "unavailable")
}

func TestGenerateShot_AFailedGenerationIsAFailedJobWithoutTakes(t *testing.T) {
	f := newFixture(t, shottest.Options{})
	f.Video.FailWith(errors.New("model down"))

	var out shotgen.ShotJobOutput
	f.CallOK("generate_shot", map[string]any{"shot_id": f.ShotID}, &out)
	st := waitJob(t, f, out.JobID)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.NotEmpty(t, st.Error)
	assert.Empty(t, st.Result["take_ids"])
}
