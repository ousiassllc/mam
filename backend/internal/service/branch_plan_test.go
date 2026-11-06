package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// A Branch made at a Scene starts before that Scene: the Shot it continues from is the last Shot
// before the Scene, not the clicked Shot, which lies after the fork and is not in the Branch's story.
func TestBranchAtAScene_ThePlanContinuesFromTheLastShotBeforeTheSceneNotTheClickedOne(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	ctx := context.Background()
	view, err := f.svc.Plans.CreateScene(ctx, f.projectID, service.SceneForm{Title: "第二幕", Location: "屋上", Time: "夜", Objective: "決着"})
	require.NoError(t, err)
	clicked, err := f.svc.Plans.CreateShot(ctx, view.Scene.ID, service.ShotForm{Objective: "クリックしたシーン内のカット", DurationSeconds: "4"})
	require.NoError(t, err)
	clickedID := clicked.Shots[len(clicked.Shots)-1].ID

	f.llm.reset(llm.AgentDirector)
	_, err = f.svc.Branch.Create(ctx, clickedID, service.BranchInput{PointType: domain.BranchAtScene, Direction: "別の決着"})
	require.NoError(t, err)
	plan := f.llm.prompts(llm.AgentDirector)[len(f.llm.prompts(llm.AgentDirector))-1]
	require.Contains(t, plan, "完成した Shot（続きの起点）")
	start := plan[strings.Index(plan, "完成した Shot（続きの起点）"):]
	assert.Contains(t, start, "主人公が振り返って走り出す", "the Shot before the Scene is where the Branch continues from")
	assert.NotContains(t, plan, "クリックしたシーン内のカット", "the clicked Shot lies after the fork")

	f.llm.reset(llm.AgentDirector)
	ref, err := repository.NewGenerationRepository(f.db).GetShot(ctx, clickedID)
	require.NoError(t, err)
	_, err = f.svc.Branch.Fork(ctx, ref, domain.BranchAtScene, 2, nil)
	require.NoError(t, err)
	for _, p := range f.llm.prompts(llm.AgentDirector) {
		assert.NotContains(t, p, "クリックしたシーン内のカット", "the proposal for the Branches does not start from the clicked Shot either")
	}
}

// When nothing precedes the Scene there is no Shot to continue from: the Branch is planned from the
// head of the Scene, without the clicked Shot (its content and its final frame).
func TestBranchAtTheFirstScene_ThePlanHasNoShotToContinueFrom(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	ctx := context.Background()
	ref := mustShotRef(t, f)
	f.llm.reset(llm.AgentDirector)
	_, err := f.svc.Branch.Create(ctx, f.shotID, service.BranchInput{PointType: domain.BranchAtScene, Direction: "最初から別展開"})
	require.NoError(t, err)
	plan := f.llm.prompts(llm.AgentDirector)[len(f.llm.prompts(llm.AgentDirector))-1]
	assert.Contains(t, plan, "これより前に完成した Shot はありません")
	assert.NotContains(t, plan, "完成した Shot（続きの起点）")
	assert.NotContains(t, plan, ref.Shot.Objective, "the clicked Shot is not the Shot before the fork")
}

func mustShotRef(t *testing.T, f genFixture) repository.ShotRef {
	t.Helper()
	ref, err := repository.NewGenerationRepository(f.db).GetShot(context.Background(), f.shotID)
	require.NoError(t, err)
	return ref
}

// Without generation a direction only names its Branch: a long one is cut to the name length, as
// the name made from a Direction is, instead of failing the whole fork after the first Branch exists.
func TestBranchFork_WithoutGenerationALongDirectionNamesItsBranchTruncated(t *testing.T) {
	f := newGenFixture(t, false)
	long := strings.Repeat("あ", 150)

	lines, err := f.svc.Branch.Fork(context.Background(), mustShotRef(t, f), domain.BranchAtShot, 3, []string{"ドアを開ける", long, "窓を見る"})
	require.NoError(t, err)
	assert.Len(t, lines, 3)
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM branch WHERE project_id = ?`, f.projectID))
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM branch WHERE name = ?`, domain.TruncateBranchName(long)))
}
