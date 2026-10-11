package service_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func fp(v float64) *float64 { return &v }

func (f genFixture) generate(t *testing.T) {
	t.Helper()
	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	require.NoError(t, err)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)
}

func TestSetProductionTemplate_ChangesTheAxesTheAgentsAreToldAndIsRecordedInTheThread(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()

	view, err := f.svc.Policy.SetProductionTemplate(ctx, f.projectID, domain.TemplateDrama)
	require.NoError(t, err)
	assert.Equal(t, domain.TemplateDrama, view.Policy.Template)
	assert.Equal(t, domain.TemplateDrama, view.Project.ProductionTemplate)
	assert.Contains(t, lastMessage(t, f.thread(t, f.projectID)).Content, "Story > Acting")

	f.generate(t)
	prompt := f.llm.prompts(llm.AgentPromptPlanner)[0]
	assert.Contains(t, prompt, "Story > Acting > Character Identity", "the Prompt Planner is told the DRAMA priorities")
	assert.NotContains(t, prompt, "Product Accuracy")

	_, err = f.svc.Policy.SetProductionTemplate(ctx, f.projectID, "movie")
	var invalid *service.ValidationError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "production_template", invalid.Field)
	_, err = f.svc.Policy.SetProductionTemplate(ctx, "missing", domain.TemplateAnime)
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestSetQualityPolicy_RoutesTheVideoModelsAndTakeCountAccordingToThePolicy(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()

	_, err := f.svc.Policy.SetQualityPolicy(ctx, f.projectID, domain.QualityFast, nil)
	require.NoError(t, err)
	f.generate(t)
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM take WHERE model_name = 'kling-3.0'`), "model-adapters.md §4: FAST -> Kling 3.0, fewer Takes")

	_, err = f.svc.Policy.SetQualityPolicy(ctx, f.projectID, domain.QualityMaxQuality, nil)
	require.NoError(t, err)
	f.generate(t)
	assert.Equal(t, 4, f.count(t, `SELECT COUNT(*) FROM take WHERE model_name = 'runway-gen-4.5'`), "MAX QUALITY -> more Takes on Runway Gen-4.5")
	var params string
	require.NoError(t, f.query(t, `SELECT model_parameters FROM take WHERE model_name = 'runway-gen-4.5' LIMIT 1`).Scan(&params))
	assert.Contains(t, params, `"720p"`)
}

func TestSetQualityPolicy_ValidatesLimitsAndAppliesThem(t *testing.T) {
	f := newGenFixture(t, false)
	ctx := context.Background()

	for field, form := range map[string]service.PolicyForm{
		"max_retries":            {MaxRetries: "11"},
		"max_generation_seconds": {MaxGenerationSeconds: "9"},
		"min_acceptance_score":   {MinAcceptanceScore: "101"},
		"min_axis_score":         {MinAxisScore: "abc"},
	} {
		_, err := f.svc.Policy.SetQualityPolicy(ctx, f.projectID, domain.QualityFast, &form)
		var invalid *service.ValidationError
		require.ErrorAs(t, err, &invalid, field)
		assert.Equal(t, field, invalid.Field)
	}
	view, err := f.svc.Policy.Policy(ctx, f.projectID)
	require.NoError(t, err)
	assert.Equal(t, domain.QualityBalanced, view.Policy.Quality, "a rejected form changes nothing")

	view, err = f.svc.Policy.SetQualityPolicy(ctx, f.projectID, domain.QualityMaxQuality,
		&service.PolicyForm{MaxRetries: "0", MaxGenerationSeconds: " 120 ", MinAcceptanceScore: "90"})
	require.NoError(t, err)
	assert.Equal(t, 0, view.Effective.MaxRetries)
	assert.Equal(t, 120*time.Second, view.Effective.MaxGenerationTime)
	assert.Equal(t, 90, view.Effective.MinAcceptanceScore)
	assert.Equal(t, domain.ProjectPolicy{Quality: domain.QualityMaxQuality}.Effective().MinAxisScore, view.Effective.MinAxisScore, "the empty field uses the policy default")

	_, err = f.svc.Policy.SetQualityPolicy(ctx, f.projectID, "slow", nil)
	assert.Error(t, err)
}

func TestSetBudget_ParsesCeilingsAndBlocksGenerationThatDoesNotFit(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()

	view, err := f.svc.Policy.SetBudget(ctx, f.projectID, service.BudgetForm{Project: "$50", Shot: "0.01"})
	require.NoError(t, err)
	assert.InDelta(t, 50, *view.Policy.ProjectBudgetCeiling, 1e-9)
	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	assert.ErrorIs(t, err, service.ErrBudgetExceeded, "the Shot ceiling blocks a generation that cannot afford one Take")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM job`))

	view, err = f.svc.Policy.SetBudget(ctx, f.projectID, service.BudgetForm{Project: "0.10"})
	require.NoError(t, err)
	assert.Nil(t, view.Policy.ShotBudgetCeiling, "an empty field removes the ceiling")
	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	assert.ErrorIs(t, err, service.ErrBudgetExceeded, "the Project ceiling blocks it too")

	for _, bad := range []string{"0", "-1", "abc", "NaN", "Inf", "1e12"} {
		_, err := f.svc.Policy.SetBudget(ctx, f.projectID, service.BudgetForm{Project: bad})
		var invalid *service.ValidationError
		require.ErrorAs(t, err, &invalid, bad)
		assert.Equal(t, "project_budget", invalid.Field)
	}
}

func TestCostSummary_ShowsEstimatedAndVendorReportedCostByShotSceneAndProject(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()
	f.models.actual = fp(0.30)

	f.generate(t) // BALANCED: 3 Takes of 4 s at 0.08 USD/s

	sum, err := f.svc.Policy.CostSummary(ctx, f.projectID)
	require.NoError(t, err)
	assert.InDelta(t, 3*0.32, sum.Project.Estimated, 1e-6)
	assert.InDelta(t, 3*0.30, sum.Project.Actual, 1e-6)
	assert.True(t, sum.Project.ActualKnown())
	require.Len(t, sum.Scenes, 1)
	require.Len(t, sum.Scenes[0].Shots, 1)
	assert.InDelta(t, sum.Project.Estimated, sum.Scenes[0].Shots[0].Totals.Estimated, 1e-9)
	assert.InDelta(t, sum.Project.Estimated, sum.Scenes[0].Totals.Estimated, 1e-9)

	_, err = f.svc.Policy.CostSummary(ctx, "missing")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestGenerateShot_StopsAtMaxGenerationTimeAndReportsIt(t *testing.T) {
	f := newGenFixture(t, true)
	f.models.block = make(chan struct{}) // the model never answers
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO project_policy (project_id, max_generation_seconds) VALUES (?, ?)`, f.projectID, domain.MinGenerationSeconds)
		return err
	}))

	start := time.Now()
	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	require.NoError(t, err)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)

	assert.Less(t, time.Since(start), 20*time.Second)
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take`))
	assert.Contains(t, lastMessage(t, f.thread(t, f.projectID)).Content, "制限時間")
}

func TestGenerateShot_WarnsBeforeStartingWhenTheDiskIsTooSmall(t *testing.T) {
	g := newGenFixtureWithFreeBytes(t, true, func(string) (uint64, error) { return 1 << 20, nil }) // 1 MiB free

	g.generate(t)

	assert.Equal(t, 3, g.count(t, `SELECT COUNT(*) FROM take`), "generation still starts: the warning does not block")
	var warned bool
	for _, m := range g.thread(t, g.projectID) {
		if m.Kind == domain.KindText && strings.Contains(m.Content, "ディスクの空き容量") {
			warned = true
			assert.Contains(t, m.Content, "/projects/"+g.projectID+"/settings")
		}
	}
	assert.True(t, warned, "the thread warns about the disk space")

	roomy := newGenFixture(t, true)
	roomy.generate(t)
	for _, m := range roomy.thread(t, roomy.projectID) {
		assert.NotContains(t, m.Content, "ディスクの空き容量")
	}
}

func TestStorage_ShowsUsageAndArchivesUnneededTakesRemovingTheirFiles(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()
	f.generate(t)
	takes := f.shot(t).Takes
	require.Len(t, takes, 3)
	files := make([]string, len(takes))
	for i, tk := range takes {
		files[i] = filepath.Join(f.assetsDir, filepath.FromSlash(tk.FilePath))
	}

	view, err := f.svc.Policy.Storage(ctx, f.projectID)
	require.NoError(t, err)
	assert.True(t, view.FreeKnown)
	assert.Equal(t, uint64(1<<40), view.FreeBytes)
	assert.Len(t, view.Takes, 3)
	assert.Positive(t, view.AssetBytes)
	n, _ := view.UnneededTakes()
	assert.Zero(t, n, "no Take is selected yet, so none is beaten")

	// The user picks Take 0; the other two are now unneeded.
	require.NoError(t, f.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE take SET status = 'selected' WHERE id = ?`, takes[0].ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE shot SET selected_take_id = ? WHERE id = ?`, takes[0].ID, f.shotID)
		return err
	}))
	view, err = f.svc.Policy.Storage(ctx, f.projectID)
	require.NoError(t, err)
	n, _ = view.UnneededTakes()
	assert.Equal(t, 2, n)

	_, err = f.svc.Policy.ArchiveTake(ctx, f.projectID, takes[0].ID)
	assert.ErrorIs(t, err, service.ErrConflict, "the Selected Take is never archived")
	var conflict *service.ConflictError
	require.ErrorAs(t, err, &conflict, "the refusal carries the reason to show")
	assert.Contains(t, conflict.Message, "アーカイブできません")

	view, err = f.svc.Policy.ArchiveTake(ctx, f.projectID, takes[1].ID)
	require.NoError(t, err)
	assert.Len(t, view.Takes, 2)
	_, err = os.Stat(files[1])
	assert.ErrorIs(t, err, os.ErrNotExist, "the video file is deleted")

	view, err = f.svc.Policy.ArchiveUnneeded(ctx, f.projectID)
	require.NoError(t, err)
	require.Len(t, view.Takes, 1)
	assert.Equal(t, takes[0].ID, view.Takes[0].TakeID)
	_, err = os.Stat(files[2])
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(files[0])
	assert.NoError(t, err, "the Selected Take keeps its file")

	_, err = f.svc.Policy.ArchiveTake(ctx, f.projectID, "missing")
	assert.ErrorIs(t, err, service.ErrNotFound)
}
