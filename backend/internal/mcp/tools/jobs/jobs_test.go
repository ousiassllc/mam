package jobs_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func newEnv(t *testing.T) *mcptest.Env {
	return mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { jobs.Register(r, env.Services) })
}

func exec(t *testing.T, env *mcptest.Env, queries ...string) {
	t.Helper()
	require.NoError(t, env.DB.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range queries {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}

func seedShot(t *testing.T, env *mcptest.Env) {
	exec(t, env,
		`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
		`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
		`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 4, 0)`,
		`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{"shot_objective":"run"}')`,
	)
}

func getJob(t *testing.T, env *mcptest.Env, id string) mcpkit.JobStatus {
	t.Helper()
	var st mcpkit.JobStatus
	env.CallOK("get_job", map[string]any{"job_id": id}, &st)
	return st
}

func TestGetJob_FollowsAShotGenerationUntilItProducedATake(t *testing.T) {
	env := newEnv(t)
	seedShot(t, env)
	ctx := context.Background()

	ref, err := env.Registrar.Jobs.Run(ctx, jobs.KindGenerateShot, "sh1", func(context.Context) (string, error) {
		exec(t, env, `UPDATE shot SET state = 'GENERATING' WHERE id = 'sh1'`)
		return "", nil
	})
	require.NoError(t, err)

	st := getJob(t, env, ref.JobID)
	assert.Equal(t, mcpkit.JobRunning, st.State)
	assert.Equal(t, "sh1", st.Target)
	assert.NotEmpty(t, st.Message)
	assert.Equal(t, st, getJob(t, env, ref.JobID), "asking again gives the same answer while nothing changed")

	exec(t, env,
		`INSERT INTO take (id, prompt_version_id, model_name, file_path) VALUES ('t1', 'pv1', 'kling-3.0', 'p1/a.mp4')`,
		`UPDATE shot SET state = 'JUDGING' WHERE id = 'sh1'`)
	st = getJob(t, env, ref.JobID)
	assert.Equal(t, mcpkit.JobRunning, st.State, "judging is still part of the work")
	assert.Equal(t, 1, st.Done)

	exec(t, env, `UPDATE shot SET state = 'EDITING' WHERE id = 'sh1'`)
	st = getJob(t, env, ref.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, st.State)
	assert.Equal(t, []any{"t1"}, st.Result["take_ids"])
	assert.Equal(t, "sh1", st.Result["shot_id"])
}

func TestGetJob_ATakeFromBeforeTheJobIsNotItsResult(t *testing.T) {
	env := newEnv(t)
	seedShot(t, env)
	exec(t, env, `INSERT INTO take (id, prompt_version_id, model_name, file_path, created_at) VALUES ('old', 'pv1', 'kling-3.0', 'p1/old.mp4', '2020-01-01T00:00:00.000Z')`)

	ref, err := env.Registrar.Jobs.Run(context.Background(), jobs.KindGenerateShot, "sh1", func(context.Context) (string, error) { return "", nil })
	require.NoError(t, err)

	st := getJob(t, env, ref.JobID)
	assert.Equal(t, mcpkit.JobFailed, st.State, "the Shot is idle and made no new Take")
	assert.Equal(t, []any{}, st.Result["take_ids"])
	assert.NotEmpty(t, st.Error)
}

func TestGetJob_AFailedQueueJobReportsItsReason(t *testing.T) {
	env := newEnv(t)
	seedShot(t, env)
	table := repository.NewJobRepository(env.DB)
	ctx := context.Background()
	row, err := table.Create(ctx, domain.JobGenerate, nil)
	require.NoError(t, err)

	ref, err := env.Registrar.Jobs.Run(ctx, jobs.KindGenerateShot, "sh1", func(context.Context) (string, error) { return row.ID, nil })
	require.NoError(t, err)
	assert.Equal(t, row.ID, ref.JobID)
	assert.Equal(t, mcpkit.JobQueued, getJob(t, env, row.ID).State)

	require.NoError(t, table.MarkFailed(ctx, row.ID, "kling: insufficient credit"))
	st := getJob(t, env, row.ID)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.Equal(t, "kling: insufficient credit", st.Error)
	assert.NotNil(t, st.FinishedAt)
}

func TestGetJob_UnknownIdIsNotFound(t *testing.T) {
	env := newEnv(t)
	assert.Contains(t, env.CallError("get_job", map[string]any{"job_id": "job_nope"}), "not_found:")
	assert.True(t, env.Call("get_job", map[string]any{}).IsError, "job_id is required")
}

func TestGetJob_ShowsTheStepsOfTheComfyUICallOfARunningShotOnly(t *testing.T) {
	env := newEnv(t)
	seedShot(t, env)
	ref, err := env.Registrar.Jobs.Run(context.Background(), jobs.KindGenerateShot, "sh1", func(context.Context) (string, error) {
		exec(t, env, `UPDATE shot SET state = 'GENERATING' WHERE id = 'sh1'`)
		return "", nil
	})
	require.NoError(t, err)
	assert.Nil(t, getJob(t, env, ref.JobID).Steps, "no step is known: no field, not a made-up zero")

	call := live.Default.Start("test-ep", "prompt-sh1")
	defer call.End()
	call.Own("sh1")
	assert.Nil(t, getJob(t, env, ref.JobID).Steps, "the server has not reported a step yet")

	call.Step("3", "KSampler", 5, 20, 30)
	st := getJob(t, env, ref.JobID)
	require.NotNil(t, st.Steps)
	assert.Equal(t, live.View{Node: "KSampler", Step: 5, MaxStep: 20, Percent: 30, ElapsedSeconds: st.Steps.ElapsedSeconds, ETAUnavailable: st.Steps.ETAUnavailable}, *st.Steps)
	assert.GreaterOrEqual(t, st.Steps.ElapsedSeconds, 0, "the time since the call was queued")

	other := live.Default.Start("test-ep", "prompt-other")
	defer other.End()
	other.Own("sh2")
	other.Step("3", "KSampler", 19, 20, 95)
	assert.Equal(t, 5, getJob(t, env, ref.JobID).Steps.Step, "another Shot's call is not this job's")

	exec(t, env, `UPDATE shot SET state = 'JUDGING' WHERE id = 'sh1'`)
	assert.Nil(t, getJob(t, env, ref.JobID).Steps, "judging is not a model call on the GPU")
}

// A queue job that failed with a failure the app knows (#1010) answers with the common error next to the reason: get_job and list_jobs say the
// kind of failure, the cause and the remedy in Japanese, the screens that help and the detail, also after a restart (the history keeps it).
func TestGetJob_AFailedQueueJobTellsTheCauseAndTheRemedy(t *testing.T) {
	env := newEnv(t)
	seedShot(t, env)
	ctx := context.Background()
	table := repository.NewJobRepository(env.DB)
	row, err := table.Create(ctx, domain.JobGenerate, nil)
	require.NoError(t, err)
	_, err = env.Registrar.Jobs.Run(ctx, jobs.KindGenerateShot, "sh1", func(context.Context) (string, error) { return row.ID, nil })
	require.NoError(t, err)
	info := apperr.New(apperr.CodeLLMKeyMissing).WithDetail("llm: every provider failed for agent producer: openai: llm: API key is not configured").Marshal()
	require.NoError(t, table.MarkFailedWithInfo(ctx, row.ID, "llm: every provider failed", info))

	check := func(t *testing.T, f *apperr.View) {
		t.Helper()
		require.NotNil(t, f)
		assert.Equal(t, apperr.CodeLLMKeyMissing, f.Code)
		assert.Equal(t, "unavailable", f.Class)
		assert.Contains(t, f.Cause, "API キーが設定されていません")
		assert.Contains(t, f.Remedy, "API キーとログイン")
		require.NotEmpty(t, f.Settings)
		assert.Equal(t, "/settings/api-keys", f.Settings[0].Path)
		assert.Contains(t, f.Detail, "API key is not configured")
	}
	st := getJob(t, env, row.ID)
	assert.Equal(t, "llm: every provider failed", st.Error, "the reason is what it was")
	check(t, st.Failure)

	after := env.Restart(register)
	var got mcpkit.JobStatus
	after.CallOK("get_job", map[string]any{"job_id": row.ID}, &got)
	check(t, got.Failure)

	listed := list(t, after, map[string]any{"state": "failed"})
	require.Len(t, listed.Jobs, 1)
	check(t, listed.Jobs[0].Failure)
}
