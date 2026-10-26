package policy_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/policy"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func newEnv(t *testing.T) (*mcptest.Env, string) {
	env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { policy.Register(r, env.Services) })
	p, err := env.Services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "春の新作スニーカーの 15 秒 PV"})
	require.NoError(t, err)
	return env, p.ID
}

func TestSetProductionTemplate_SwitchesTheTemplateAndRejectsAnUnknownOne(t *testing.T) {
	env, id := newEnv(t)

	var out policy.Policy
	env.CallOK("set_production_template", map[string]any{"project_id": id, "production_template": "anime"}, &out)
	assert.Equal(t, "anime", out.ProductionTemplate)
	assert.Contains(t, out.EvaluationAxes, "Character Design")

	var settings policy.SettingsOutput
	env.CallOK("get_project_settings", map[string]any{"project_id": id}, &settings)
	assert.Equal(t, "anime", settings.ProductionTemplate, "the screen's read shows the change")

	assert.Contains(t, env.CallError("set_production_template", map[string]any{"project_id": id, "production_template": "opera"}), "validation:")
	assert.Contains(t, env.CallError("set_production_template", map[string]any{"project_id": "missing", "production_template": "drama"}), "not_found:")
}

func TestSetQualityPolicy_LimitsAreOptionalAndCheckedLikeTheForm(t *testing.T) {
	env, id := newEnv(t)

	var out policy.Policy
	env.CallOK("set_quality_policy", map[string]any{
		"project_id": id, "quality_policy": "max_quality",
		"limits": map[string]any{"max_retries": 7, "min_acceptance_score": 90},
	}, &out)
	assert.Equal(t, "max_quality", out.QualityPolicy)
	assert.Equal(t, 7, out.Effective.MaxRetries)
	assert.Equal(t, 90, out.Effective.MinAcceptanceScore)
	assert.Equal(t, 600, out.Effective.MaxGenerationSeconds, "an omitted limit is the default of the Quality Policy")

	// Without limits, the stored overrides stay (the composer's switch does the same).
	env.CallOK("set_quality_policy", map[string]any{"project_id": id, "quality_policy": "fast"}, &out)
	assert.Equal(t, "fast", out.QualityPolicy)
	require.NotNil(t, out.Limits.MaxRetries)
	assert.Equal(t, 7, *out.Limits.MaxRetries)

	// With limits but a field left out, that field goes back to the default.
	out = policy.Policy{} // a field the answer omits must not keep the previous value
	env.CallOK("set_quality_policy", map[string]any{"project_id": id, "quality_policy": "fast", "limits": map[string]any{}}, &out)
	assert.Nil(t, out.Limits.MaxRetries)
	assert.Equal(t, 1, out.Effective.MaxRetries)

	msg := env.CallError("set_quality_policy", map[string]any{"project_id": id, "quality_policy": "fast", "limits": map[string]any{"max_retries": 11}})
	assert.Contains(t, msg, "validation:")
	assert.Contains(t, msg, "max_retries")
	assert.Contains(t, env.CallError("set_quality_policy", map[string]any{"project_id": id, "quality_policy": "ultra"}), "validation:")

	env.CallOK("get_project_settings", map[string]any{"project_id": id}, &out)
	assert.Equal(t, 1, out.Effective.MaxRetries, "a refused request changed nothing")
}

func TestSetProjectBudget_SetsAndRemovesCeilings(t *testing.T) {
	env, id := newEnv(t)

	var out policy.Policy
	env.CallOK("set_project_budget", map[string]any{"project_id": id, "project_budget_usd": 120.5, "shot_budget_usd": 3}, &out)
	require.NotNil(t, out.ProjectBudgetUSD)
	assert.InDelta(t, 120.5, *out.ProjectBudgetUSD, 1e-9)
	require.NotNil(t, out.ShotBudgetUSD)
	assert.InDelta(t, 3, *out.ShotBudgetUSD, 1e-9)

	var cost policy.CostSummaryOutput
	env.CallOK("get_cost_summary", map[string]any{"project_id": id}, &cost)
	require.NotNil(t, cost.ProjectCeilingUSD)
	assert.InDelta(t, 120.5, *cost.ProjectCeilingUSD, 1e-9)
	assert.NotNil(t, cost.Scenes)

	out = policy.Policy{}
	env.CallOK("set_project_budget", map[string]any{"project_id": id, "shot_budget_usd": 3}, &out)
	assert.Nil(t, out.ProjectBudgetUSD, "an omitted ceiling is removed, as an empty field is on the screen")
	assert.NotNil(t, out.ShotBudgetUSD)

	for _, v := range []float64{0, -1, 1_000_001} {
		msg := env.CallError("set_project_budget", map[string]any{"project_id": id, "project_budget_usd": v})
		assert.Contains(t, msg, "validation:", "%v", v)
		assert.Contains(t, msg, "project_budget")
	}
	assert.Contains(t, env.CallError("set_project_budget", map[string]any{"project_id": "missing"}), "not_found:")
	assert.Contains(t, env.CallError("get_cost_summary", map[string]any{"project_id": "missing"}), "not_found:")
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

// seedTakes gives the Project one Shot with three stored Takes; t1 is Selected.
func seedTakes(t *testing.T, env *mcptest.Env, projectID string) []string {
	t.Helper()
	var files []string
	exec(t, env,
		`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', '`+projectID+`', 'S', 0)`,
		`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 4, 0)`,
		`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{}')`,
	)
	require.NoError(t, os.MkdirAll(filepath.Join(env.AssetsDir, projectID), 0o750))
	for _, id := range []string{"t1", "t2", "t3"} {
		rel := projectID + "/" + id + ".mp4"
		require.NoError(t, os.WriteFile(filepath.Join(env.AssetsDir, filepath.FromSlash(rel)), []byte("video-"+id), 0o600))
		files = append(files, filepath.Join(env.AssetsDir, filepath.FromSlash(rel)))
		exec(t, env,
			`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES ('a-`+id+`', '`+projectID+`', 'video', '`+rel+`', 'video/mp4', 8)`,
			`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path) VALUES ('`+id+`', 'pv1', 'kling-3.0', '{}', '`+rel+`')`,
		)
	}
	exec(t, env, `UPDATE take SET status = 'selected' WHERE id = 't1'`, `UPDATE shot SET selected_take_id = 't1' WHERE id = 'sh1'`)
	return files
}

func TestArchive_FreesOnlyTakesTheScreenWouldAndKeepsTheSelectedOne(t *testing.T) {
	env, id := newEnv(t)
	files := seedTakes(t, env, id)

	var usage policy.StorageOutput
	env.CallOK("get_storage_usage", map[string]any{"project_id": id}, &usage)
	require.Len(t, usage.Takes, 3)
	assert.Equal(t, 2, usage.UnneededCount)
	assert.EqualValues(t, 16, usage.UnneededBytes)
	for _, tk := range usage.Takes {
		assert.Equal(t, tk.TakeID != "t1", tk.Archivable, tk.TakeID)
	}

	msg := env.CallError("archive_take", map[string]any{"project_id": id, "take_id": "t1"})
	assert.Contains(t, msg, "conflict:", "the Selected Take is never archived")
	assert.Contains(t, msg, "アーカイブできません")
	assert.FileExists(t, files[0])

	env.CallOK("archive_take", map[string]any{"project_id": id, "take_id": "t2"}, &usage)
	assert.Len(t, usage.Takes, 2)
	assert.NoFileExists(t, files[1], "the video file is deleted")
	assert.Contains(t, env.CallError("archive_take", map[string]any{"project_id": id, "take_id": "t2"}), "not_found:")
	assert.Contains(t, env.CallError("archive_take", map[string]any{"project_id": id, "take_id": "t3-of-no-one"}), "not_found:")

	env.CallOK("archive_unneeded_takes", map[string]any{"project_id": id}, &usage)
	require.Len(t, usage.Takes, 1)
	assert.Equal(t, "t1", usage.Takes[0].TakeID)
	assert.NoFileExists(t, files[2])
	assert.FileExists(t, files[0], "the Selected Take keeps its file")

	env.CallOK("archive_unneeded_takes", map[string]any{"project_id": id}, &usage) // nothing left to free: not an error
	assert.Len(t, usage.Takes, 1)
	assert.Contains(t, env.CallError("archive_unneeded_takes", map[string]any{"project_id": "missing"}), "not_found:")
	assert.Contains(t, env.CallError("get_storage_usage", map[string]any{"project_id": "missing"}), "not_found:")
}

func TestArchiveTake_RefusesATakeOfAnotherProject(t *testing.T) {
	env, id := newEnv(t)
	files := seedTakes(t, env, id)
	other, err := env.Services.Projects.Create(context.Background(), service.CreateProjectInput{Content: "別の Project"})
	require.NoError(t, err)

	assert.Contains(t, env.CallError("archive_take", map[string]any{"project_id": other.ID, "take_id": "t2"}), "not_found:")
	assert.FileExists(t, files[1])
}
