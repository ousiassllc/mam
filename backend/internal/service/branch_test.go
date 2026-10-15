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

func (f genFixture) branch(t *testing.T, in service.BranchInput) service.BranchDetail {
	t.Helper()
	d, err := f.svc.Branch.Create(context.Background(), f.shotID, in)
	require.NoError(t, err)
	return d
}

func (f genFixture) trunkShots(t *testing.T) int {
	t.Helper()
	return f.count(t, `SELECT COUNT(*) FROM shot sh JOIN scene s ON s.id = sh.scene_id WHERE s.project_id = ? AND s.branch_id IS NULL`, f.projectID)
}

func TestBranchCreate_SavesTheBranchPointAndSnapshotsTheWorldStateWithoutTouchingTheTrunk(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	trunk, err := f.svc.World.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	require.NotZero(t, trunk.World.Version)

	d := f.branch(t, service.BranchInput{Name: "  ドアを開ける "})

	assert.Equal(t, "ドアを開ける", d.Branch.Name)
	assert.Equal(t, domain.BranchAtShot, d.Branch.PointType, "shot is the default Branch Point")
	assert.Equal(t, f.shotID, d.Branch.PointShotID)
	assert.False(t, d.Started)
	assert.Equal(t, 1, d.LatestWorldVersion)
	require.Len(t, d.Versions, 1)

	view, err := f.svc.World.View(context.Background(), f.projectID, d.Branch.ID)
	require.NoError(t, err)
	assert.Equal(t, d.Branch.ID, view.Branch.ID)
	assert.Equal(t, len(trunk.World.Characters), len(view.World.Characters), "the Snapshot holds the World State of the Shot")
	after, err := f.svc.World.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, trunk.World.Version, after.World.Version, "the trunk gained no version")

	_, err = f.svc.World.View(context.Background(), f.projectID, "missing")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestBranchCreate_FrameNeedsAnOffsetInsideTheShotAndNamesAreNumbered(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.director = directorFor(sameSceneAnswer)
	ctx := context.Background()

	_, err := f.svc.Branch.Create(ctx, f.shotID, service.BranchInput{PointType: domain.BranchAtFrame})
	var ve *service.ValidationError
	require.ErrorAs(t, err, &ve)
	_, err = f.svc.Branch.Create(ctx, f.shotID, service.BranchInput{PointType: domain.BranchAtFrame, OffsetSeconds: "999"})
	require.ErrorAs(t, err, &ve)
	_, err = f.svc.Branch.Create(ctx, f.shotID, service.BranchInput{PointType: "moon"})
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, "point_type", ve.Field, "the error names the form field the handler reads")
	_, err = f.svc.Branch.Create(ctx, "missing", service.BranchInput{})
	assert.ErrorIs(t, err, service.ErrNotFound)

	first := f.branch(t, service.BranchInput{PointType: domain.BranchAtFrame, OffsetSeconds: "1.5"})
	assert.InDelta(t, 1.5, first.Branch.PointOffsetSeconds, 0.001)
	assert.Equal(t, "Branch 1", first.Branch.Name)
	assert.Equal(t, "Branch 2", f.branch(t, service.BranchInput{PointType: domain.BranchAtScene}).Branch.Name)

	_, err = f.svc.Branch.Create(ctx, f.shotID, service.BranchInput{Direction: "逃げる"})
	assert.NoError(t, err, "a Direction is accepted while generation is configured")
	f2 := newGenFixture(t, false)
	_, err = f2.svc.Branch.Create(ctx, f2.shotID, service.BranchInput{Direction: "逃げる"})
	assert.ErrorIs(t, err, service.ErrUnavailable, "the AI cannot plan a Shot without generation")
}

func TestBranchCreate_WithADirectionPlansAndGeneratesTheFirstShotInTheBranchOnly(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	trunkBefore, in, plan, refused := f.trunkShots(t), service.BranchInput{Direction: "彼女がドアを開ける"}, f.llm.director, error(nil)
	call := func() error { _, err := f.svc.Branch.Create(context.Background(), f.shotID, in); return err }
	f.llm.director = func(system, user string) string { refused = f.overlap(call); return plan(system, user) } // an identical request while the Shot is being planned (a double click)
	d := f.branch(t, in)
	assert.ErrorIs(t, refused, service.ErrConflict)
	assert.True(t, d.Started)
	assert.Equal(t, "彼女がドアを開ける", d.Branch.Name, "the Branch is named after its direction")
	require.Len(t, d.Shots, 1)
	assert.Equal(t, "ドアを開けて廊下を見る", d.Shots[0].Shot.Objective)
	require.Len(t, d.Scenes, 1)
	assert.Equal(t, d.Branch.ID, d.Scenes[0].BranchID)
	assert.Equal(t, trunkBefore, f.trunkShots(t), "the trunk keeps its Shots")
	// The first Shot of the Branch is generated from, and updates, the Branch's own World State.
	f.waitGenerateJobs(t, 3) // Reference Pack and Take of the trunk Shot, then the Take of the Branch Shot
	view, err := f.svc.World.View(context.Background(), f.projectID, d.Branch.ID)
	require.NoError(t, err)
	trunk, err := f.svc.World.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	assert.Greater(t, view.World.Version, 1, "the Shot of the Branch appended a version to the Branch")
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM state_diff d JOIN world_state_version v ON v.id = d.world_state_version_id WHERE v.branch_id = ?`, d.Branch.ID))
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM state_diff d JOIN world_state_version v ON v.id = d.world_state_version_id WHERE v.branch_id IS NULL`), "no diff of the Branch reached the trunk")
	assert.Equal(t, 1, trunk.World.Version)
}

func TestBranchUpdate_RenamesAndMovesTheWorldStateReferenceAndAppendsVersions(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	ctx := context.Background()
	d := f.branch(t, service.BranchInput{Name: "A"})
	view, err := f.svc.World.View(ctx, f.projectID, d.Branch.ID)
	require.NoError(t, err)
	require.NotEmpty(t, view.World.Characters)
	_, err = f.svc.World.UpdateEntity(ctx, f.projectID, d.Branch.ID, domain.TargetCharacter, view.World.Characters[0].ID, map[string]string{"emotion": "sad"})
	require.NoError(t, err)

	name, version := "Renamed", "2"
	d, err = f.svc.Branch.Update(ctx, d.Branch.ID, &name, &version)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", d.Branch.Name)
	assert.Equal(t, 2, d.Branch.WorldStateVersion)
	assert.Equal(t, 2, d.LatestWorldVersion)
	assert.Len(t, d.Versions, 2, "the change is appended, the first version stays")

	var ve *service.ValidationError
	bad, big := "9", "0"
	_, err = f.svc.Branch.Update(ctx, d.Branch.ID, nil, &bad)
	assert.ErrorAs(t, err, &ve, "a version the Branch does not have")
	_, err = f.svc.Branch.Update(ctx, d.Branch.ID, nil, &big)
	assert.ErrorAs(t, err, &ve)
	_, err = f.svc.Branch.Update(ctx, d.Branch.ID, nil, nil)
	assert.ErrorAs(t, err, &ve)
	empty := "  "
	_, err = f.svc.Branch.Update(ctx, d.Branch.ID, &empty, nil)
	assert.ErrorAs(t, err, &ve)
	_, err = f.svc.Branch.Update(ctx, "missing", &name, nil)
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestBranchTree_NestsABranchMadeAtAShotOfAnotherBranch(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	parent := f.branch(t, service.BranchInput{Direction: "彼女がドアを開ける"})
	f.waitGenerateJobs(t, 3)

	_, err := f.svc.Branch.Create(context.Background(), parent.Shots[0].Shot.ID, service.BranchInput{Name: "child"})
	require.NoError(t, err)
	other := f.branch(t, service.BranchInput{Name: "sibling"})

	tree, err := f.svc.Branch.Tree(context.Background(), f.projectID)
	require.NoError(t, err)
	require.Len(t, tree.Roots, 2)
	assert.Equal(t, parent.Branch.ID, tree.Roots[0].ID)
	require.Len(t, tree.Roots[0].Children, 1)
	assert.Equal(t, "child", tree.Roots[0].Children[0].Name)
	assert.Equal(t, other.Branch.ID, tree.Roots[1].ID)
	labels := make([]string, len(tree.Shots))
	for i, s := range tree.Shots {
		labels[i] = s.Label
	}
	assert.Contains(t, labels, "Scene 1 / Shot 1")
	assert.Contains(t, labels, "彼女がドアを開ける: Scene 1 / Shot 1", "a Shot of a Branch is labelled with its Branch")

	_, err = f.svc.Branch.Tree(context.Background(), "missing")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestChat_DirectorModeMakesSeveralBranchesFromAFreeDirection(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	f.startDirecting(t)
	f.llm.interpreter = opsJSON(`{"type":"create_branch","count":3,"point_type":"shot","directions":["ドアを開ける"]}`)
	f.llm.director = directorFor(sameSceneAnswer)

	ws := f.say(t, "このカットから別展開を3つ作って", "")

	msg := ws.Messages[len(ws.Messages)-1].Content
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM branch WHERE project_id = ?`, f.projectID))
	assert.Contains(t, msg, "ドアを開ける")
	assert.Contains(t, msg, "Scene 1 / Shot 1 の後")
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM branch WHERE name = ?`, "ドアを開ける"), "named directions come first")
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM branch WHERE name IN (?, ?, ?, ?)`, "ドアを開ける", "彼女がドアを開ける", "背後から声が聞こえる", "突然照明が消える"),
		"the team proposes the developments the director left open")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM pragma_table_info('chat_message') WHERE name = 'branch_id'`), "one thread per Project: chat is not split by Branch")
}

func TestChat_DirectorModeGivesEveryBranchItsFirstShotEvenWhenFewerDevelopmentsAreProposed(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	f.startDirecting(t)
	f.llm.interpreter = opsJSON(`{"type":"create_branch","count":5,"point_type":"shot"}`)
	f.llm.director = directorFor(sameSceneAnswer)

	ws := f.say(t, "このカットから別展開を5つ作って", "")

	msg := ws.Messages[len(ws.Messages)-1].Content
	assert.Equal(t, 5, f.count(t, `SELECT COUNT(*) FROM branch WHERE project_id = ?`, f.projectID))
	assert.Equal(t, 5, f.count(t, `SELECT COUNT(DISTINCT s.branch_id) FROM scene s JOIN shot sh ON sh.scene_id = s.id WHERE s.branch_id <> ''`),
		"the three proposed developments and the two left to the AI each have a first Shot")
	assert.Equal(t, 5, strings.Count(msg, "最初の Shot の生成を始めました"))
}

func (f genFixture) forbid(t *testing.T, text string) {
	t.Helper()
	_, err := f.svc.Brand.Add(context.Background(), f.projectID, service.BrandForm{Type: string(domain.BrandForbidden), Text: text})
	require.NoError(t, err)
}

// A Direction with a forbidden expression of a LOCK Brand constraint is refused before the Branch
// or its Shot exists (spec §23), not after they were committed.
func TestBranchCreate_ADirectionWithAForbiddenExpressionMakesNothing(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	f.forbid(t, "最高")
	trunkBefore := f.trunkShots(t)

	_, err := f.svc.Branch.Create(context.Background(), f.shotID, service.BranchInput{Direction: "最高の展開"})

	require.ErrorIs(t, err, domain.ErrBrandViolation)
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM branch WHERE project_id = ?`, f.projectID))
	assert.Equal(t, trunkBefore, f.trunkShots(t))
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM scene WHERE project_id = ? AND branch_id IS NOT NULL`, f.projectID))
}

// The Direction is bounded like the one of Generate / Continue (1000 characters): an overlong one is a
// 422 before the Branch exists, and the chat's Fork cuts it instead of refusing the whole request.
func TestBranchCreate_AnOverlongDirectionIsRefusedBeforeTheBranchExists(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)

	_, err := f.svc.Branch.Create(context.Background(), f.shotID, service.BranchInput{Direction: strings.Repeat("あ", 1001)})

	var verr *service.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "direction", verr.Field)
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM branch WHERE project_id = ?`, f.projectID))
	d := f.branch(t, service.BranchInput{Direction: strings.Repeat("あ", 1000)})
	assert.True(t, d.Started, "1000 characters are accepted")
}

func TestBranchFork_ALongDirectionIsCutAndStillStartsTheBranch(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)

	lines, err := f.svc.Branch.Fork(context.Background(), mustShotRef(t, f), domain.BranchAtShot, 1, []string{strings.Repeat("あ", 1500)})

	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "最初の Shot の生成を始めました")
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM branch WHERE project_id = ?`, f.projectID))
}

func TestChat_DirectorModeABranchDirectionWithAForbiddenExpressionMakesNothing(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	f.forbid(t, "最高")
	f.startDirecting(t)
	f.llm.director = directorFor(sameSceneAnswer)

	f.llm.interpreter = opsJSON(`{"type":"create_branch","count":2,"point_type":"shot","directions":["ドアを開ける","最高の別展開"]}`)
	_, err := f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "このカットから別展開を2つ作って"})
	require.ErrorIs(t, err, domain.ErrBrandViolation)
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM branch WHERE project_id = ?`, f.projectID), "not even the clean direction's Branch")

	f.llm.interpreter = opsJSON(`{"type":"create_branch","count":1,"point_type":"shot","directions":["ドアを開ける"]}`)
	_, err = f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "最高の別展開を作って"})
	require.ErrorIs(t, err, domain.ErrBrandViolation, "the words of the message too")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM branch WHERE project_id = ?`, f.projectID))
}

// A Branch is planned from the story up to its Branch Point: the Shots of the trunk after the fork
// are not part of it (generation works from the same cut series).
func TestBranch_ThePlanOfTheFirstShotDoesNotSeeTheTrunkShotsAfterTheFork(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	_, err := f.svc.Plans.CreateShot(context.Background(), f.sceneID, service.ShotForm{Objective: "トランクだけの後続カット", DurationSeconds: "4"})
	require.NoError(t, err)

	f.branch(t, service.BranchInput{Direction: "ドアを開ける"})
	plan := f.llm.prompts(llm.AgentDirector)[len(f.llm.prompts(llm.AgentDirector))-1]
	assert.Contains(t, plan, "主人公が振り返って走り出す", "the Shots up to the fork are the story so far")
	assert.NotContains(t, plan, "トランクだけの後続カット")

	f.llm.reset(llm.AgentDirector)
	_, err = f.svc.Branch.Fork(context.Background(), mustShotRef(t, f), domain.BranchAtShot, 2, nil)
	require.NoError(t, err)
	for _, p := range f.llm.prompts(llm.AgentDirector) {
		assert.NotContains(t, p, "トランクだけの後続カット", "the proposal for the Branches is cut at the fork as well")
	}
}

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
