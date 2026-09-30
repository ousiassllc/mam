package repository_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func wideSpec(objective string) domain.ShotSpec {
	return domain.ShotSpec{ShotObjective: objective, Camera: domain.SpecCamera{Framing: "wide"}}
}

func shotObjective(t *testing.T, f genRepoFixture) string {
	t.Helper()
	ref, err := f.repo.GetShot(context.Background(), f.shotID)
	require.NoError(t, err)
	spec, ok := ref.Shot.CanonicalSpec()
	require.True(t, ok)
	return spec.ShotObjective
}

func TestVersionRepository_RestoreShotVersionAppendsACopyAndKeepsHistory(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	versions := repository.NewVersionRepository(f.db)
	_, err := f.repo.SavePromptedSpec(ctx, f.shotID, wideSpec("first"), nil)
	require.NoError(t, err)
	_, err = f.repo.SavePromptedSpec(ctx, f.shotID, wideSpec("second"), json.RawMessage(`{"n":2}`))
	require.NoError(t, err)

	pv, err := versions.RestoreShotVersion(ctx, f.shotID, 1)
	require.NoError(t, err)

	assert.Equal(t, 3, pv.VersionNumber, "a restore is a new version, nothing is overwritten")
	assert.Equal(t, "first", shotObjective(t, f))
	list, err := versions.ShotVersions(ctx, f.shotID)
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, 3, list[0].Number, "newest first")
	assert.Equal(t, "first", list[0].Summary)
	assert.Equal(t, "second", list[1].Summary)
	var output sql.NullString
	require.NoError(t, f.db.Reader().QueryRow(`SELECT model_adapter_output FROM prompt_version WHERE shot_id = ? AND version_number = 2`, f.shotID).Scan(&output))
	assert.JSONEq(t, `{"n":2}`, output.String, "the old version keeps what it recorded")
}

func TestVersionRepository_RestoreShotVersionRefusals(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	versions := repository.NewVersionRepository(f.db)
	_, err := f.repo.SavePromptedSpec(ctx, f.shotID, wideSpec("first"), nil)
	require.NoError(t, err)
	_, err = f.repo.SavePromptedSpec(ctx, f.shotID, wideSpec("second"), nil)
	require.NoError(t, err)

	_, err = versions.RestoreShotVersion(ctx, f.shotID, 9)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = versions.RestoreShotVersion(ctx, "missing", 1)
	assert.ErrorIs(t, err, repository.ErrNotFound)

	require.NoError(t, exec(t, f.db, `INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES ('l1', ?, 'shot', ?, 'prompt', 'lock', 'null')`, f.projectID, f.shotID))
	_, err = versions.RestoreShotVersion(ctx, f.shotID, 1)
	assert.ErrorIs(t, err, repository.ErrLocked, "Prompt is LOCKed: the spec must not change")
	assert.Equal(t, "second", shotObjective(t, f))
	require.NoError(t, exec(t, f.db, `DELETE FROM lock`))

	started, err := f.repo.StartShot(ctx, f.shotID)
	require.NoError(t, err)
	require.True(t, started)
	_, err = versions.RestoreShotVersion(ctx, f.shotID, 1)
	assert.ErrorIs(t, err, repository.ErrConflict, "a Shot being generated cannot be rewound")

	list, err := versions.ShotVersions(ctx, f.shotID)
	require.NoError(t, err)
	assert.Len(t, list, 2, "refused restores write nothing")
	_, err = versions.ShotVersions(ctx, "missing")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestVersionRepository_RestoreWorldStateBringsTheEntityFieldsBackAsANewVersion(t *testing.T) {
	ctx := context.Background()
	f := newWorldFixture(t)
	versions := repository.NewVersionRepository(f.db)
	_, v1, err := f.world.ApplyStateDiff(ctx, f.projectID, "", f.shotID, f.takeID, domain.StateDiff{Changes: []domain.StateChange{
		{Target: domain.TargetCharacter, ID: f.charID, Field: "emotion", To: raw(`"anxious"`)},
		{Target: domain.TargetCamera, Field: "movement", To: raw(`"static"`)},
	}})
	require.NoError(t, err)
	_, _, err = f.world.ApplyStateDiff(ctx, f.projectID, "", f.shotID, f.takeID, domain.StateDiff{Changes: []domain.StateChange{
		{Target: domain.TargetCharacter, ID: f.charID, Field: "emotion", To: raw(`"angry"`)},
		{Target: domain.TargetCamera, Field: "movement", To: raw(`"pan"`)},
		{Target: domain.TargetProduct, ID: f.prodID, Field: "condition", To: raw(`"opened"`)},
	}})
	require.NoError(t, err)
	newChar := domain.NewID()
	require.NoError(t, exec(t, f.db, `INSERT INTO character (id, project_id, name, emotion) VALUES (?, ?, 'Late', 'happy')`, newChar, f.projectID))

	n, err := versions.RestoreWorldStateVersion(ctx, f.projectID, "", v1)
	require.NoError(t, err)

	assert.Equal(t, 3, n)
	w, err := f.world.World(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, 3, w.Version)
	byID := map[string]domain.WorldCharacter{}
	for _, c := range w.Characters {
		byID[c.ID] = c
	}
	assert.Equal(t, "anxious", byID[f.charID].Emotion)
	assert.Equal(t, "happy", byID[newChar].Emotion, "entities the version does not know are left alone")
	assert.Equal(t, "static", w.Camera.Movement)
	assert.Equal(t, "unopened", w.Products[0].Condition, "the Product went back to its state in version 1")
	list, err := versions.WorldStateVersions(ctx, f.projectID, "")
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, "Character 2 / Product 1 / Location 0", list[0].Summary)
}

func TestVersionRepository_RestoreWorldStateRefusesToChangeALockedFieldAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	f := newWorldFixture(t)
	versions := repository.NewVersionRepository(f.db)
	_, v1, err := f.world.ApplyStateDiff(ctx, f.projectID, "", f.shotID, f.takeID, domain.StateDiff{Changes: []domain.StateChange{
		{Target: domain.TargetCharacter, ID: f.charID, Field: "emotion", To: raw(`"anxious"`)},
	}})
	require.NoError(t, err)
	require.NoError(t, f.world.UpdateEntity(ctx, f.projectID, "", domain.TargetCharacter, f.charID, map[string]string{"wardrobe": "red_dress", "emotion": "angry"}))
	f.lock(t, "character", f.charID, "wardrobe")

	_, err = versions.RestoreWorldStateVersion(ctx, f.projectID, "", v1)

	assert.ErrorIs(t, err, repository.ErrLocked)
	w, err := f.world.World(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, "angry", w.Characters[0].Emotion, "the emotion was not written either: all or nothing")
	assert.Equal(t, 2, w.Version)

	_, err = versions.RestoreWorldStateVersion(ctx, f.projectID, "", 99)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = versions.RestoreWorldStateVersion(ctx, "missing", "", 1)
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

// seedBranch inserts a Branch of the Project.
func seedBranch(t *testing.T, f genRepoFixture, id, name string) {
	t.Helper()
	require.NoError(t, exec(t, f.db,
		`INSERT INTO branch (id, project_id, branch_point_type, branch_point_shot_id, name) VALUES (?, ?, 'shot', ?, ?)`, id, f.projectID, f.shotID, name))
}

func TestVersionRepository_RestoringABranchWorldStateWritesItsOwnRowsAndLeavesTheTrunkAlone(t *testing.T) {
	ctx := context.Background()
	f := newWorldFixture(t)
	versions := repository.NewVersionRepository(f.db)
	branches := repository.NewBranchRepository(f.db)
	_, _, err := f.world.ApplyStateDiff(ctx, f.projectID, "", f.shotID, f.takeID, domain.StateDiff{Changes: []domain.StateChange{
		{Target: domain.TargetCharacter, ID: f.charID, Field: "emotion", To: raw(`"anxious"`)},
	}})
	require.NoError(t, err)
	b, err := branches.Create(ctx, repository.NewBranch{ShotID: f.shotID, PointType: domain.BranchAtShot, Name: "alt"})
	require.NoError(t, err)
	w, err := f.world.World(ctx, f.projectID, b.ID)
	require.NoError(t, err)
	require.NoError(t, f.world.UpdateEntity(ctx, f.projectID, b.ID, domain.TargetCharacter, w.Characters[0].ID, map[string]string{"emotion": "sad"}))
	trunkBefore, err := versions.WorldStateVersions(ctx, f.projectID, "")
	require.NoError(t, err)

	n, err := versions.RestoreWorldStateVersion(ctx, f.projectID, b.ID, 1)
	require.NoError(t, err)

	assert.Equal(t, 3, n, "the restore is a new version of the Branch's own series")
	w, err = f.world.World(ctx, f.projectID, b.ID)
	require.NoError(t, err)
	assert.Equal(t, "anxious", w.Characters[0].Emotion, "the Branch's rows are written back")
	list, err := versions.WorldStateVersions(ctx, f.projectID, b.ID)
	require.NoError(t, err)
	assert.Len(t, list, 3)
	trunk, err := versions.WorldStateVersions(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Len(t, trunk, len(trunkBefore), "the trunk series does not see the Branch's versions")

	_, err = versions.RestoreWorldStateVersion(ctx, f.projectID, "nope", 1)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	assert.Error(t, exec(t, f.db, `INSERT INTO world_state_version (id, project_id, version_number, state) VALUES ('dup', ?, 1, '{}')`, f.projectID),
		"a trunk version number is unique although its branch_id is NULL")
}

func TestVersionRepository_RestoreBranchVersionAppendsACopyAndResetsTheBranch(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	versions := repository.NewVersionRepository(f.db)
	seedBranch(t, f, "b1", "renamed")
	require.NoError(t, exec(t, f.db, `INSERT INTO branch_version (id, branch_id, version_number, name) VALUES ('v1', 'b1', 1, 'original'), ('v2', 'b1', 2, 'renamed')`))

	n, err := versions.RestoreBranchVersion(ctx, "b1", 1)
	require.NoError(t, err)

	assert.Equal(t, 3, n)
	var name string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT name FROM branch WHERE id = 'b1'`).Scan(&name))
	assert.Equal(t, "original", name)
	list, err := versions.BranchVersions(ctx, "b1")
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, "original", list[0].Summary)
	assert.Equal(t, "renamed", list[1].Summary)

	_, err = versions.RestoreBranchVersion(ctx, "b1", 9)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = versions.BranchVersions(ctx, "missing")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}
