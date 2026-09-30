package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

type worldFixture struct {
	genRepoFixture
	world  *repository.WorldRepository
	takeID string
	charID string
	prodID string
}

func newWorldFixture(t *testing.T) worldFixture {
	t.Helper()
	ctx := context.Background()
	f := worldFixture{genRepoFixture: newGenRepoFixture(t)}
	f.world = repository.NewWorldRepository(f.db)
	pv, err := f.repo.SavePromptedSpec(ctx, f.shotID, domain.ShotSpec{ShotObjective: "x", Camera: domain.SpecCamera{Framing: "wide"}}, nil, nil)
	require.NoError(t, err)
	f.takeID, err = f.repo.CreateTake(ctx, repository.NewTake{
		ProjectID: f.projectID, ShotID: f.shotID, PromptVersionID: pv.ID, Model: domain.ModelSeedance2,
		Asset: domain.Asset{ID: "asset-1", ProjectID: f.projectID, Type: domain.AssetVideo, FilePath: "p/asset-1.mp4", MimeType: "video/mp4"},
	})
	require.NoError(t, err)
	f.charID, f.prodID = domain.NewID(), domain.NewID()
	require.NoError(t, exec(t, f.db, `INSERT INTO character (id, project_id, name, wardrobe, emotion) VALUES (?, ?, 'Mio', 'black_coat', 'calm')`, f.charID, f.projectID))
	require.NoError(t, exec(t, f.db, `INSERT INTO product (id, project_id, name, condition) VALUES (?, ?, 'Serum', 'unopened')`, f.prodID, f.projectID))
	return f
}

func (f worldFixture) lock(t *testing.T, target, id, param string) {
	t.Helper()
	require.NoError(t, exec(t, f.db,
		`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES (?, ?, ?, ?, ?, 'lock', 'null')`,
		domain.NewID(), f.projectID, target, id, param))
}

func (f worldFixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.db.Reader().QueryRow(q, args...).Scan(&n))
	return n
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestWorldRepository_ApplyStateDiffAppendsVersionsAndKeepsEarlierOnes(t *testing.T) {
	ctx := context.Background()
	f := newWorldFixture(t)

	w, err := f.world.World(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Zero(t, w.Version, "no Shot has completed yet")

	applied, v1, err := f.world.ApplyStateDiff(ctx, f.projectID, "", f.shotID, f.takeID, domain.StateDiff{Changes: []domain.StateChange{
		{Target: domain.TargetCharacter, ID: f.charID, Field: "emotion", To: raw(`"anxious"`)},
		{Target: domain.TargetCamera, Field: "movement", To: raw(`"static"`)},
		{Target: domain.TargetStory, Field: "known_information", To: raw(`["heard_noise"]`)},
	}})
	require.NoError(t, err)
	assert.Equal(t, 1, v1)
	require.Len(t, applied.Changes, 3)
	assert.JSONEq(t, `"calm"`, string(applied.Changes[0].From), "the diff records the value it replaced")

	_, v2, err := f.world.ApplyStateDiff(ctx, f.projectID, "", f.shotID, f.takeID, domain.StateDiff{Changes: []domain.StateChange{
		{Target: domain.TargetCharacter, ID: f.charID, Field: "position", To: raw(`"hallway"`)},
	}})
	require.NoError(t, err)
	assert.Equal(t, 2, v2)

	w, err = f.world.World(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, 2, w.Version)
	assert.Equal(t, "anxious", w.Characters[0].Emotion)
	assert.Equal(t, "hallway", w.Characters[0].Position)
	assert.Equal(t, "black_coat", w.Characters[0].Wardrobe, "untouched fields stay")
	assert.Equal(t, "static", w.Camera.Movement, "Camera and Story carry over to the next version")
	assert.Equal(t, []string{"heard_noise"}, w.Story.KnownInformation)

	// Nothing is overwritten: both versions exist and the first still shows the state of Shot 1.
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM world_state_version WHERE project_id = ?`, f.projectID))
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM state_diff WHERE shot_id = ?`, f.shotID))
	var first string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT state FROM world_state_version WHERE version_number = 1`).Scan(&first))
	assert.Contains(t, first, `"emotion":"anxious"`)
	assert.NotContains(t, first, "hallway")
}

func TestWorldRepository_ApplyStateDiffSkipsLockedFieldsMissingEntitiesAndUnknownFields(t *testing.T) {
	ctx := context.Background()
	f := newWorldFixture(t)
	f.lock(t, "character", f.charID, "wardrobe")
	f.lock(t, "camera", "", "lens")

	applied, _, err := f.world.ApplyStateDiff(ctx, f.projectID, "", f.shotID, f.takeID, domain.StateDiff{Changes: []domain.StateChange{
		{Target: domain.TargetCharacter, ID: f.charID, Field: "wardrobe", To: raw(`"red_dress"`)},
		{Target: domain.TargetCamera, Field: "lens", To: raw(`"85mm"`)},
		{Target: domain.TargetCharacter, ID: "missing", Field: "emotion", To: raw(`"happy"`)},
		{Target: domain.TargetCharacter, ID: f.charID, Field: "personality", To: raw(`"cheerful"`)},
		{Target: domain.TargetCharacter, ID: f.charID, Field: "emotion", To: raw(`["not text"]`)},
		{Target: domain.TargetProduct, ID: f.prodID, Field: "condition", To: raw(`"opened"`)},
	}})
	require.NoError(t, err)
	require.Len(t, applied.Changes, 1, "only the unlocked, known change is applied")
	assert.Equal(t, "condition", applied.Changes[0].Field)

	w, err := f.world.World(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, "black_coat", w.Characters[0].Wardrobe, "LOCKed wardrobe is untouched")
	assert.Empty(t, w.Characters[0].Personality, "the State Extractor never sets personality")
	assert.Empty(t, w.Camera.Lens, "the singleton Camera lens is LOCKed")
	assert.Equal(t, "opened", w.Products[0].Condition)
}

func TestWorldRepository_UpdateEntityWritesFieldsAppendsVersionAndRefusesLocks(t *testing.T) {
	ctx := context.Background()
	f := newWorldFixture(t)

	require.NoError(t, f.world.UpdateEntity(ctx, f.projectID, "", domain.TargetCharacter, f.charID, map[string]string{"hairstyle": "long_black", "personality": "reserved"}))
	w, err := f.world.World(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, "long_black", w.Characters[0].Hairstyle)
	assert.Equal(t, "reserved", w.Characters[0].Personality)
	assert.Equal(t, 1, w.Version, "a user edit is a new World State version too")

	f.lock(t, "character", f.charID, "wardrobe")
	err = f.world.UpdateEntity(ctx, f.projectID, "", domain.TargetCharacter, f.charID, map[string]string{"hairstyle": "short", "wardrobe": "red_dress"})
	assert.ErrorIs(t, err, repository.ErrLocked)
	w, err = f.world.World(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, "long_black", w.Characters[0].Hairstyle, "all or none: the unlocked field of a refused update is not written either")
	assert.Equal(t, 1, w.Version)

	assert.ErrorIs(t, f.world.UpdateEntity(ctx, f.projectID, "", domain.TargetCharacter, "missing", map[string]string{"hairstyle": "x"}), repository.ErrNotFound)
	assert.ErrorIs(t, f.world.UpdateEntity(ctx, "other-project", "", domain.TargetCharacter, f.charID, map[string]string{"hairstyle": "x"}), repository.ErrNotFound, "an entity of another Project is not found")
	assert.Error(t, f.world.UpdateEntity(ctx, f.projectID, "", domain.TargetProduct, f.prodID, map[string]string{"name": "x"}), "only editable fields are written")
}

func TestWorldRepository_AutoModeLocksDoNotBlock(t *testing.T) {
	ctx := context.Background()
	f := newWorldFixture(t)
	require.NoError(t, exec(t, f.db,
		`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES (?, ?, 'character', ?, 'wardrobe', 'auto', 'null')`,
		domain.NewID(), f.projectID, f.charID))
	require.NoError(t, f.world.UpdateEntity(ctx, f.projectID, "", domain.TargetCharacter, f.charID, map[string]string{"wardrobe": "red_dress"}))
}

func TestGenerationRepository_SaveReferencePackRegistersCharactersAndProductsOnce(t *testing.T) {
	ctx := context.Background()
	f := newWorldFixture(t)
	pack := func(suffix string) repository.NewReferencePack {
		p := repository.NewReferencePack{ProjectID: f.projectID, ShotID: f.shotID}
		for i, r := range []domain.Reference{
			{Kind: domain.RefCharacter, Name: "Mio"}, // already there
			{Kind: domain.RefCharacter, Name: "Ren"},
			{Kind: domain.RefProduct, Name: "Toner"},
			{Kind: domain.RefLocation, Name: "Street"}, // Locations come from the Scenes
			{Kind: domain.RefStyle},
		} {
			id := fmt.Sprintf("a%d-%s", i, suffix)
			r.AssetID = id
			p.References = append(p.References, r)
			p.Assets = append(p.Assets, domain.Asset{ID: id, ProjectID: f.projectID, Type: domain.AssetReference, FilePath: "p/" + id + ".png", MimeType: "image/png"})
		}
		return p
	}
	require.NoError(t, f.repo.SaveReferencePack(ctx, pack("one")))
	require.NoError(t, f.repo.SaveReferencePack(ctx, pack("two")), "a rebuild does not duplicate them")

	w, err := f.world.World(ctx, f.projectID, "")
	require.NoError(t, err)
	require.Len(t, w.Characters, 2)
	assert.Equal(t, "Mio", w.Characters[0].Name)
	assert.Equal(t, "Ren", w.Characters[1].Name)
	require.Len(t, w.Products, 2)
	assert.Empty(t, w.Locations)
}
