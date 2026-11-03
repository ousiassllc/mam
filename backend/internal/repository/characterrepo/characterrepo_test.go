package characterrepo_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/characterrepo"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

type fixture struct {
	db     *repository.DB
	world  *repository.WorldRepository
	series *repository.SeriesRepository
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := testdb.Open(t)
	f := fixture{db: db, world: repository.NewWorldRepository(db), series: repository.NewSeriesRepository(db)}
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'key_check', 'product_cm', 'balanced')`,
			`INSERT INTO character (id, project_id, name) VALUES ('c1', 'p1', 'Mio')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	return f
}

func TestInWorld_StoresItAppendsAVersionAndRefusesAnUsedNameOrAGhost(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	require.NoError(t, characterrepo.InWorld(ctx, f.world, "p1", "", domain.WorldCharacter{ID: "c-new", Name: "Aoi", Hairstyle: "ponytail", FaceReferenceAssetID: ""}))
	w, err := f.world.World(ctx, "p1", "")
	require.NoError(t, err)
	require.Len(t, w.Characters, 2)
	assert.Equal(t, 1, w.Version, "the World State has a new version")
	for _, c := range w.Characters {
		if c.ID == "c-new" {
			assert.Equal(t, "Aoi", c.Name)
			assert.Equal(t, "ponytail", c.Hairstyle)
		}
	}

	assert.ErrorIs(t, characterrepo.InWorld(ctx, f.world, "p1", "", domain.WorldCharacter{ID: "c-dup", Name: " mio "}), repository.ErrConflict, "Mio is there")
	assert.ErrorIs(t, characterrepo.InWorld(ctx, f.world, "nope", "", domain.WorldCharacter{ID: "c-x", Name: "X"}), repository.ErrNotFound)
	assert.ErrorIs(t, characterrepo.InWorld(ctx, f.world, "p1", "no-branch", domain.WorldCharacter{ID: "c-x", Name: "X"}), repository.ErrNotFound)
	w, err = f.world.World(ctx, "p1", "")
	require.NoError(t, err)
	assert.Len(t, w.Characters, 2, "a refused call writes nothing")
	assert.Equal(t, 1, w.Version)
}

func TestInCanon_AddsToTheCanonAsANewVersionAndRefusesAnUsedNameOrAGhost(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	s, err := f.series.Create(ctx, "Empty", "")
	require.NoError(t, err)

	require.NoError(t, characterrepo.InCanon(ctx, f.series, s.ID, domain.WorldCharacter{ID: "k1", Name: "Kana", Wardrobe: "coat", Emotion: "angry"}))
	canon, err := f.series.Canon(ctx, s.ID)
	require.NoError(t, err)
	require.Len(t, canon.Characters, 1)
	assert.Equal(t, "k1", canon.Characters[0].ID)
	assert.Equal(t, "coat", canon.Characters[0].Wardrobe)
	assert.Empty(t, canon.Characters[0].Emotion, "what a Shot leaves is not canon")
	got, err := f.series.Get(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.Version, "the canon has a new version")

	assert.ErrorIs(t, characterrepo.InCanon(ctx, f.series, s.ID, domain.WorldCharacter{ID: "k2", Name: "KANA"}), repository.ErrConflict)
	assert.ErrorIs(t, characterrepo.InCanon(ctx, f.series, "nope", domain.WorldCharacter{ID: "k3", Name: "X"}), repository.ErrNotFound)
	got, err = f.series.Get(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.Version, "a refused call appends no version")
}
