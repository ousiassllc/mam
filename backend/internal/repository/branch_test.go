package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

type branchFixture struct {
	worldFixture
	branches *repository.BranchRepository
	plans    *repository.PlanRepository
}

func newBranchFixture(t *testing.T) branchFixture {
	t.Helper()
	f := branchFixture{worldFixture: newWorldFixture(t)}
	f.branches = repository.NewBranchRepository(f.db)
	f.plans = repository.NewPlanRepository(f.db)
	return f
}

func (f branchFixture) diff(t *testing.T, branchID string, changes ...domain.StateChange) int {
	t.Helper()
	_, v, err := f.world.ApplyStateDiff(context.Background(), f.projectID, branchID, f.shotID, f.takeID, domain.StateDiff{Changes: changes})
	require.NoError(t, err)
	return v
}

func (f branchFixture) emotion(t *testing.T, branchID string) string {
	t.Helper()
	w, err := f.world.World(context.Background(), f.projectID, branchID)
	require.NoError(t, err)
	require.Len(t, w.Characters, 1)
	return w.Characters[0].Emotion
}

func TestBranchRepository_CreateSnapshotsTheWorldStateOfTheShotNotTheLaterOne(t *testing.T) {
	ctx := context.Background()
	f := newBranchFixture(t)
	f.diff(t, "", domain.StateChange{Target: domain.TargetCharacter, ID: f.charID, Field: "emotion", To: raw(`"anxious"`)})
	f.diff(t, "", domain.StateChange{Target: domain.TargetCharacter, ID: f.charID, Field: "emotion", To: raw(`"angry"`)})

	// The Shot's latest diff is the one that counts: a Branch at a Shot starts from what the Shot left.
	b, err := f.branches.Create(ctx, repository.NewBranch{ShotID: f.shotID, PointType: domain.BranchAtShot, Name: "A"})
	require.NoError(t, err)
	assert.Equal(t, "angry", f.emotion(t, b.ID))
	assert.Equal(t, 1, b.WorldStateVersion, "the Branch numbers its own World State versions from 1")
	assert.Equal(t, 1, b.Version)
	assert.Equal(t, domain.BranchAtShot, b.PointType)
	assert.Equal(t, f.shotID, b.PointShotID)
	assert.Equal(t, 1, b.PointSceneNumber)
	assert.Equal(t, 1, b.PointShotNumber)
	assert.Empty(t, b.ParentID)

	frame, err := f.branches.Create(ctx, repository.NewBranch{ShotID: f.shotID, PointType: domain.BranchAtFrame, OffsetSeconds: 1.5, Name: "F"})
	require.NoError(t, err)
	assert.InDelta(t, 1.5, frame.PointOffsetSeconds, 0.001)

	_, err = f.branches.Create(ctx, repository.NewBranch{ShotID: "missing", PointType: domain.BranchAtShot, Name: "x"})
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestBranchRepository_BranchChangesNeverReachTheTrunkOrAnotherBranch(t *testing.T) {
	ctx := context.Background()
	f := newBranchFixture(t)
	f.diff(t, "", domain.StateChange{Target: domain.TargetCharacter, ID: f.charID, Field: "emotion", To: raw(`"anxious"`)})
	a, err := f.branches.Create(ctx, repository.NewBranch{ShotID: f.shotID, PointType: domain.BranchAtShot, Name: "A"})
	require.NoError(t, err)
	b, err := f.branches.Create(ctx, repository.NewBranch{ShotID: f.shotID, PointType: domain.BranchAtShot, Name: "B"})
	require.NoError(t, err)

	aw, err := f.world.World(ctx, f.projectID, a.ID)
	require.NoError(t, err)
	require.Len(t, aw.Characters, 1)
	assert.NotEqual(t, f.charID, aw.Characters[0].ID, "a Branch has its own Character rows")

	// Edit on Branch A: the trunk and Branch B keep their values, and the trunk row is not addressable from A.
	require.NoError(t, f.world.UpdateEntity(ctx, f.projectID, a.ID, domain.TargetCharacter, aw.Characters[0].ID, map[string]string{"emotion": "furious", "wardrobe": "red"}))
	assert.Equal(t, "furious", f.emotion(t, a.ID))
	assert.Equal(t, "anxious", f.emotion(t, ""))
	assert.Equal(t, "anxious", f.emotion(t, b.ID))
	assert.ErrorIs(t, f.world.UpdateEntity(ctx, f.projectID, a.ID, domain.TargetCharacter, f.charID, map[string]string{"emotion": "x"}), repository.ErrNotFound)

	// A State Diff applied on Branch B and on the trunk stays where it was applied.
	f.diff(t, b.ID, domain.StateChange{Target: domain.TargetCamera, Field: "movement", To: raw(`"dolly"`)})
	f.diff(t, "", domain.StateChange{Target: domain.TargetStory, Field: "previous_event", To: raw(`"trunk_event"`)})
	bw, _ := f.world.World(ctx, f.projectID, b.ID)
	tw, _ := f.world.World(ctx, f.projectID, "")
	aw, _ = f.world.World(ctx, f.projectID, a.ID)
	assert.Equal(t, "dolly", bw.Camera.Movement)
	assert.Empty(t, tw.Camera.Movement)
	assert.Empty(t, aw.Camera.Movement)
	assert.Empty(t, bw.Story.PreviousEvent)
	assert.Equal(t, "trunk_event", tw.Story.PreviousEvent)

	// Every series counts its own versions.
	assert.Equal(t, 2, bw.Version)
	assert.Equal(t, 2, tw.Version)
	assert.Equal(t, 2, aw.Version)
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM character WHERE project_id = ? AND branch_id IS NULL`, f.projectID))

	_, err = f.world.World(ctx, f.projectID, "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestBranchRepository_LocksAreCopiedAndScopedToTheBranchEntity(t *testing.T) {
	ctx := context.Background()
	f := newBranchFixture(t)
	f.lock(t, "character", f.charID, "wardrobe")
	a, err := f.branches.Create(ctx, repository.NewBranch{ShotID: f.shotID, PointType: domain.BranchAtShot, Name: "A"})
	require.NoError(t, err)
	aw, _ := f.world.World(ctx, f.projectID, a.ID)
	id := aw.Characters[0].ID

	assert.ErrorIs(t, f.world.UpdateEntity(ctx, f.projectID, a.ID, domain.TargetCharacter, id, map[string]string{"wardrobe": "x"}), repository.ErrLocked, "the LOCK follows the entity into the Branch")
	require.NoError(t, f.world.SetFieldLock(ctx, f.projectID, a.ID, domain.TargetCharacter, id, "wardrobe", domain.LockAuto))
	require.NoError(t, f.world.UpdateEntity(ctx, f.projectID, a.ID, domain.TargetCharacter, id, map[string]string{"wardrobe": "x"}))
	assert.ErrorIs(t, f.world.UpdateEntity(ctx, f.projectID, "", domain.TargetCharacter, f.charID, map[string]string{"wardrobe": "y"}), repository.ErrLocked, "unlocking in the Branch leaves the trunk LOCKed")
}

func TestBranchRepository_UpdateAppendsBranchVersionsAndKeepsTheWorldStateSeries(t *testing.T) {
	ctx := context.Background()
	f := newBranchFixture(t)
	b, err := f.branches.Create(ctx, repository.NewBranch{ShotID: f.shotID, PointType: domain.BranchAtShot, Name: "A"})
	require.NoError(t, err)
	aw, _ := f.world.World(ctx, f.projectID, b.ID)
	require.NoError(t, f.world.UpdateEntity(ctx, f.projectID, b.ID, domain.TargetCharacter, aw.Characters[0].ID, map[string]string{"emotion": "sad"}))

	name := "Renamed"
	b, err = f.branches.Update(ctx, b.ID, repository.BranchChange{Name: &name})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", b.Name)
	assert.Equal(t, 2, b.Version)
	assert.Equal(t, 1, b.WorldStateVersion, "a rename does not move the World State reference")

	v2 := 2
	b, err = f.branches.Update(ctx, b.ID, repository.BranchChange{WorldStateVersion: &v2})
	require.NoError(t, err)
	assert.Equal(t, 2, b.WorldStateVersion)
	assert.Equal(t, 3, b.Version)

	same, err := f.branches.Update(ctx, b.ID, repository.BranchChange{Name: &name, WorldStateVersion: &v2})
	require.NoError(t, err)
	assert.Equal(t, 3, same.Version, "a change that alters nothing appends no version")

	missing := 9
	_, err = f.branches.Update(ctx, b.ID, repository.BranchChange{WorldStateVersion: &missing})
	assert.ErrorIs(t, err, repository.ErrVersionNotFound)
	_, err = f.branches.Update(ctx, "nope", repository.BranchChange{Name: &name})
	assert.ErrorIs(t, err, repository.ErrNotFound)

	versions, err := f.branches.Versions(ctx, b.ID)
	require.NoError(t, err)
	require.Len(t, versions, 3)
	assert.Equal(t, []string{"Renamed", "Renamed", "A"}, []string{versions[0].Name, versions[1].Name, versions[2].Name}, "newest first, earlier names kept")
	assert.Equal(t, []int{2, 1, 1}, []int{versions[0].WorldStateVersion, versions[1].WorldStateVersion, versions[2].WorldStateVersion})
}

func TestBranchRepository_ScenesOfABranchAreSeparateFromTheTrunk(t *testing.T) {
	ctx := context.Background()
	f := newBranchFixture(t)
	b, err := f.branches.Create(ctx, repository.NewBranch{ShotID: f.shotID, PointType: domain.BranchAtShot, Name: "A"})
	require.NoError(t, err)

	scene, err := f.plans.CreateScene(ctx, domain.Scene{ProjectID: f.projectID, BranchID: b.ID, Title: "Door", Location: "Hall"})
	require.NoError(t, err)
	assert.Equal(t, 0, scene.OrderIndex, "the Branch numbers its own Scenes from the first")
	shot, err := f.plans.CreateShot(ctx, domain.Shot{SceneID: scene.ID, Objective: "open", Duration: 2})
	require.NoError(t, err)

	trunk, err := f.plans.ListScenes(ctx, f.projectID)
	require.NoError(t, err)
	require.Len(t, trunk, 1)
	assert.Equal(t, "A", trunk[0].Title)
	own, err := f.branches.Scenes(ctx, b.ID)
	require.NoError(t, err)
	require.Len(t, own, 1)
	assert.Equal(t, scene.ID, own[0].ID)
	assert.Equal(t, b.ID, own[0].BranchID)

	// The Shots the Branch's story shows: the trunk up to the Branch Point, then its own tree.
	gens := repository.NewGenerationRepository(f.db)
	story, err := gens.StoryShots(ctx, f.projectID, b.ID)
	require.NoError(t, err)
	require.Len(t, story, 2)
	assert.Equal(t, f.shotID, story[0].Shot.ID)
	assert.Equal(t, shot.ID, story[1].Shot.ID)
	trunkShots, err := gens.ListProjectShots(ctx, f.projectID)
	require.NoError(t, err)
	assert.Len(t, trunkShots, 2, "the trunk keeps its own two Shots")
	ref, err := gens.GetShot(ctx, shot.ID)
	require.NoError(t, err)
	assert.Equal(t, b.ID, ref.Scene.BranchID)

	// HasScenes and SavePlan see the trunk only.
	has, err := f.plans.HasScenes(ctx, f.projectID)
	require.NoError(t, err)
	assert.True(t, has)
}

func TestBranchRepository_BranchAtSceneStartsFromTheStateBeforeTheScene(t *testing.T) {
	ctx := context.Background()
	f := newBranchFixture(t)
	f.diff(t, "", domain.StateChange{Target: domain.TargetCharacter, ID: f.charID, Field: "emotion", To: raw(`"anxious"`)})
	second, err := f.plans.CreateScene(ctx, domain.Scene{ProjectID: f.projectID, Title: "B"})
	require.NoError(t, err)
	shot, err := f.plans.CreateShot(ctx, domain.Shot{SceneID: second.ID, Objective: "later", Duration: 2})
	require.NoError(t, err)
	// Shot 1 of Scene 2 completes and changes the emotion; a Branch at Scene 2 must not see it.
	pv, err := f.repo.SavePromptedSpec(ctx, shot.ID, domain.ShotSpec{ShotObjective: "x", Camera: domain.SpecCamera{Framing: "wide"}}, nil, nil)
	require.NoError(t, err)
	take2, err := f.repo.CreateTake(ctx, repository.NewTake{
		ProjectID: f.projectID, ShotID: shot.ID, PromptVersionID: pv.ID, Model: domain.ModelSeedance2,
		Asset: domain.Asset{ID: "asset-2", ProjectID: f.projectID, Type: domain.AssetVideo, FilePath: "p/asset-2.mp4", MimeType: "video/mp4"},
	})
	require.NoError(t, err)
	_, _, err = f.world.ApplyStateDiff(ctx, f.projectID, "", shot.ID, take2, domain.StateDiff{Changes: []domain.StateChange{
		{Target: domain.TargetCharacter, ID: f.charID, Field: "emotion", To: raw(`"furious"`)},
	}})
	require.NoError(t, err)

	atScene, err := f.branches.Create(ctx, repository.NewBranch{ShotID: shot.ID, PointType: domain.BranchAtScene, Name: "S"})
	require.NoError(t, err)
	assert.Equal(t, "anxious", f.emotion(t, atScene.ID))
	assert.Equal(t, second.ID, atScene.PointSceneID)
	assert.Equal(t, 2, atScene.PointSceneNumber)
	assert.Zero(t, atScene.PointShotNumber)

	atShot, err := f.branches.Create(ctx, repository.NewBranch{ShotID: shot.ID, PointType: domain.BranchAtShot, Name: "T"})
	require.NoError(t, err)
	assert.Equal(t, "furious", f.emotion(t, atShot.ID))

	nested, err := f.branches.Create(ctx, repository.NewBranch{ShotID: mustFirstShot(t, f, atShot.ID), PointType: domain.BranchAtShot, Name: "N"})
	require.NoError(t, err)
	assert.Equal(t, atShot.ID, nested.ParentID, "a Branch made at a Shot of a Branch hangs from that Branch")
}

func mustFirstShot(t *testing.T, f branchFixture, branchID string) string {
	t.Helper()
	ctx := context.Background()
	scene, err := f.plans.CreateScene(ctx, domain.Scene{ProjectID: f.projectID, BranchID: branchID, Title: "own"})
	require.NoError(t, err)
	shot, err := f.plans.CreateShot(ctx, domain.Shot{SceneID: scene.ID, Objective: "own", Duration: 1})
	require.NoError(t, err)
	return shot.ID
}
