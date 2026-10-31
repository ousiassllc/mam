package jobs_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func register(env *mcptest.Env, r *mcpkit.Registrar) { jobs.Register(r, env.Services) }

func TestGetJob_ANonQueueJobIsAnsweredAfterARestartAsBefore(t *testing.T) {
	env := newEnv(t)
	seedShot(t, env)
	ctx := context.Background()
	ref, err := env.Registrar.Jobs.Run(ctx, jobs.KindGenerateShot, "sh1", func(context.Context) (string, error) {
		exec(t, env, `UPDATE shot SET state = 'GENERATING' WHERE id = 'sh1'`)
		return "", nil
	})
	require.NoError(t, err)
	assert.Contains(t, ref.JobID, "job_", "the service queued no job row")
	before := getJob(t, env, ref.JobID)
	require.Equal(t, mcpkit.JobRunning, before.State)

	after := env.Restart(register)
	var got mcpkit.JobStatus
	after.CallOK("get_job", map[string]any{"job_id": ref.JobID}, &got)
	assert.Equal(t, before, got, "the same state, target, Project and start time")
	assert.Equal(t, "p1", got.ProjectID)

	exec(t, env,
		`INSERT INTO take (id, prompt_version_id, model_name, file_path) VALUES ('t1', 'pv1', 'kling-3.0', 'p1/a.mp4')`,
		`UPDATE shot SET state = 'EDITING' WHERE id = 'sh1'`)
	got = mcpkit.JobStatus{} // decoding into a used struct keeps the fields the answer leaves out
	after.CallOK("get_job", map[string]any{"job_id": ref.JobID}, &got)
	assert.Equal(t, mcpkit.JobSucceeded, got.State, "the Probe reads the persisted Shot after the restart too")
	assert.Equal(t, []any{"t1"}, got.Result["take_ids"])
	require.NotNil(t, got.EndedAt)

	again := after.Restart(register)
	var last mcpkit.JobStatus
	again.CallOK("get_job", map[string]any{"job_id": ref.JobID}, &last)
	assert.Equal(t, got, last)
}

func TestGetJob_AQueueJobKeepsItsAnswerAcrossARestart(t *testing.T) {
	env := newEnv(t)
	seedShot(t, env)
	ctx := context.Background()
	table := repository.NewJobRepository(env.DB)
	row, err := table.Create(ctx, domain.JobGenerate, nil)
	require.NoError(t, err)
	_, err = env.Registrar.Jobs.Run(ctx, jobs.KindGenerateShot, "sh1", func(context.Context) (string, error) { return row.ID, nil })
	require.NoError(t, err)
	require.NoError(t, table.MarkFailed(ctx, row.ID, "kling: insufficient credit"))
	before := getJob(t, env, row.ID)

	after := env.Restart(register)
	var got mcpkit.JobStatus
	after.CallOK("get_job", map[string]any{"job_id": row.ID}, &got)
	assert.Equal(t, before, got)
	assert.Equal(t, "kling: insufficient credit", got.Error)
	assert.Equal(t, "sh1", got.Target, "the target the history kept, which the row does not have")
}

func repositoryJob(env *mcptest.Env) *repository.JobRepository {
	return repository.NewJobRepository(env.DB)
}
