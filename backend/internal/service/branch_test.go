package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
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
	trunkBefore := f.trunkShots(t)

	d := f.branch(t, service.BranchInput{Direction: "彼女がドアを開ける"})

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
