package mainwait_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// #1108: the diagnosed state, read through the real services. A Project in VISUAL_DESIGN whose Shots are all PLANNING with no Take, an empty
// Reference Library and no Character: the hierarchy view says the main line waits, how many Shots, and what is missing.
func TestHierarchy_VisualDesignWithUnstartedShotsExplainsTheWait(t *testing.T) {
	testenv.Parallel(t)
	ctx := context.Background()
	db := testdb.Open(t)
	svc := servicetest.New(t, service.Deps{DB: db, AssetsDir: filepath.Join(t.TempDir(), "assets")})
	p, err := svc.Projects.Create(ctx, service.CreateProjectInput{Content: "美容液の縦型15秒CMを作りたい"})
	require.NoError(t, err)

	view, err := svc.Plans.Hierarchy(ctx, p.ID, "", "")
	require.NoError(t, err)
	assert.False(t, view.Wait.Active(), "a DRAFT Project has no Shots to wait on")

	projects, plans := repository.NewProjectRepository(db), repository.NewPlanRepository(db)
	require.NoError(t, projects.Transition(ctx, p.ID, domain.StatePlanning))
	require.NoError(t, projects.Transition(ctx, p.ID, domain.StateVisualDesign))
	plan := domain.ScenePlan{Scenes: []domain.PlannedScene{
		{Title: "朝", Shots: []domain.PlannedShot{{Objective: "光", DurationSeconds: 3}, {Objective: "目覚め", DurationSeconds: 2}}},
		{Title: "夜", Shots: []domain.PlannedShot{{Objective: "眠る", DurationSeconds: 4}}},
	}}
	board := domain.Storyboard{Shots: [][]domain.ShotStoryboard{{{Description: "窓"}, {Description: "目"}}, {{Description: "枕"}}}}
	require.NoError(t, plans.SavePlan(ctx, p.ID, plan, board))

	view, err = svc.Plans.Hierarchy(ctx, p.ID, "", "")
	require.NoError(t, err)
	assert.True(t, view.Wait.Active())
	assert.Equal(t, 3, view.Wait.Shots)
	assert.True(t, view.Wait.Counted)
	assert.Zero(t, view.Wait.References)
	assert.Zero(t, view.Wait.Characters)
	text := view.Wait.Text()
	assert.Contains(t, text, "Shot 3 件が PLANNING")
	assert.Contains(t, text, "Reference Library が 0 件")
	assert.Contains(t, text, "Character）が 0 人")
	assert.Contains(t, text, "次に押すもの")
	assert.Contains(t, text, "生成は有料なので自動では始まりません")
}
