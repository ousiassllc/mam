package characterdraft_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/characterdraft"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/characterrepo"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
)

type fixture struct {
	projects *repository.ProjectRepository
	world    *repository.WorldRepository
	maker    *characterdraft.Maker
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := testdb.Open(t)
	f := fixture{projects: repository.NewProjectRepository(db), world: repository.NewWorldRepository(db)}
	f.maker = characterdraft.New(f.projects, repository.NewAssetRepository(db), f.world, nil)
	return f
}

func asset(projectID string, typ domain.AssetType, size int64) domain.Asset {
	return domain.Asset{ID: domain.NewID(), ProjectID: projectID, Type: typ, FilePath: projectID + "/" + domain.NewID() + ".png", MimeType: "image/png", SizeBytes: size}
}

// project is a Project whose Brief holds the assets (in this order).
func (f fixture) project(t *testing.T, assets ...func(id string) domain.Asset) (string, []domain.Asset) {
	t.Helper()
	id := domain.NewID()
	var made []domain.Asset
	brief := domain.Brief{Subject: "美容液"}
	for _, a := range assets {
		made = append(made, a(id))
		brief.Assets = append(brief.Assets, made[len(made)-1].ID)
	}
	require.NoError(t, f.projects.Create(context.Background(), repository.NewProject{
		Project: domain.Project{ID: id, State: domain.StateDraft, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced},
		Brief: brief, Assets: made,
	}))
	return id, made
}

func image(size int64) func(string) domain.Asset {
	return func(id string) domain.Asset { return asset(id, domain.AssetImage, size) }
}

func TestMake_DraftsACharacterForEachAttachedImageWithTheImageAsItsFace(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id, assets := f.project(t, image(10), image(20))

	res, err := f.maker.Make(ctx, id)

	require.NoError(t, err)
	assert.Equal(t, 2, res.Created)
	assert.Equal(t, 2, res.Drafts)
	assert.Empty(t, res.Skipped)
	w, err := f.world.World(ctx, id, "")
	require.NoError(t, err)
	require.Len(t, w.Characters, 2)
	assert.Equal(t, 1, w.Version, "the World State has one new version")
	byName := map[string]domain.WorldCharacter{}
	for _, c := range w.Characters {
		byName[c.Name] = c
	}
	for i, a := range assets {
		c := byName["登場人物 "+string(rune('1'+i))]
		assert.Equal(t, a.ID, c.FaceReferenceAssetID, "the attached image is the face")
		assert.True(t, c.Draft)
		assert.Empty(t, c.Hairstyle+c.Wardrobe+c.Voice+c.Personality+c.BodyReferenceAssetID, "what an image does not tell stays empty")
	}
}

func TestMake_RunningItAgainAddsNothing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id, _ := f.project(t, image(10), image(20))
	_, err := f.maker.Make(ctx, id)
	require.NoError(t, err)

	res, err := f.maker.Make(ctx, id)

	require.NoError(t, err)
	assert.Zero(t, res.Created, "the drafts are there already")
	assert.Equal(t, 2, res.Drafts, "they are still told")
	w, err := f.world.World(ctx, id, "")
	require.NoError(t, err)
	assert.Len(t, w.Characters, 2)
	assert.Equal(t, 1, w.Version, "no version is appended")
}

func TestMake_DoesNotDraftWhereTheProjectHasACharacterAlready(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id, _ := f.project(t, image(10))
	require.NoError(t, characterrepo.InWorld(ctx, f.world, id, "", domain.WorldCharacter{ID: "c1", Name: "Mio"}))

	res, err := f.maker.Make(ctx, id)

	require.NoError(t, err)
	assert.Zero(t, res.Created)
	assert.Zero(t, res.Drafts, "Mio is no draft")
	w, err := f.world.World(ctx, id, "")
	require.NoError(t, err)
	require.Len(t, w.Characters, 1)
	assert.Equal(t, "Mio", w.Characters[0].Name)
}

func TestMake_LeavesOutWhatIsNoImageOrTooBigAndSaysWhy(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id, assets := f.project(t, image(10),
		func(id string) domain.Asset { return asset(id, domain.AssetVideo, 10) },
		image(characterdraft.MaxImageBytes+1))

	res, err := f.maker.Make(ctx, id)

	require.NoError(t, err)
	assert.Equal(t, 1, res.Created)
	require.Len(t, res.Skipped, 2)
	assert.Contains(t, res.Skipped[0], assets[1].ID)
	assert.Contains(t, res.Skipped[0], "画像ではない")
	assert.Contains(t, res.Skipped[1], assets[2].ID)
	assert.Contains(t, res.Skipped[1], "サイズ")
	w, err := f.world.World(ctx, id, "")
	require.NoError(t, err)
	require.Len(t, w.Characters, 1)
	assert.Equal(t, assets[0].ID, w.Characters[0].FaceReferenceAssetID)
}

func TestMake_WithoutAnImageMakesNothingAndLeavesNoVersion(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id, _ := f.project(t)

	res, err := f.maker.Make(ctx, id)

	require.NoError(t, err)
	assert.Equal(t, characterdraft.Result{}, res)
	w, err := f.world.World(ctx, id, "")
	require.NoError(t, err)
	assert.Zero(t, w.Version)
}

func TestMake_UnknownProjectIsNotFound(t *testing.T) {
	_, err := newFixture(t).maker.Make(context.Background(), "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestMaxImageBytes_IsTheUploadLimit(t *testing.T) {
	assert.EqualValues(t, service.MaxImageBytes, characterdraft.MaxImageBytes)
}

func TestMake_AnEditOfTheDraftClearsItsMark(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id, _ := f.project(t, image(10))
	_, err := f.maker.Make(ctx, id)
	require.NoError(t, err)
	w, err := f.world.World(ctx, id, "")
	require.NoError(t, err)
	require.Len(t, w.Characters, 1)

	require.NoError(t, f.world.UpdateEntity(ctx, id, "", domain.TargetCharacter, w.Characters[0].ID, map[string]string{"hairstyle": "bob"}))

	w, err = f.world.World(ctx, id, "")
	require.NoError(t, err)
	assert.False(t, w.Characters[0].Draft)
	assert.Equal(t, "bob", w.Characters[0].Hairstyle)
	res, err := f.maker.Make(ctx, id)
	require.NoError(t, err)
	assert.Zero(t, res.Created, "an edited draft is a Character: nothing is drafted again")
	assert.Zero(t, res.Drafts)
}
