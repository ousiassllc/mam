package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestBrandTemplateRepository_SaveSnapshotsTheConstraintsAndApplyCopiesThemIndependently(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedProject(t, db, "p1")
	seedProject(t, db, "p2")
	brands := repository.NewBrandRepository(db)
	templates := repository.NewBrandTemplateRepository(db)
	lock, err := brands.Create(ctx, domain.BrandConstraint{ProjectID: "p1", Type: domain.BrandForbidden, Level: domain.EnforceLock, Text: "最安値\n激安"})
	require.NoError(t, err)
	_, err = brands.Create(ctx, domain.BrandConstraint{ProjectID: "p1", Type: domain.BrandDisclaimer, Level: domain.EnforceAdvisory, Text: "効果には個人差があります"})
	require.NoError(t, err)

	saved, err := templates.SaveFromProject(ctx, "ブランドA", "p1")
	require.NoError(t, err)
	require.Len(t, saved.Constraints, 2)

	// The template is a snapshot: the Project changing afterwards does not reach it.
	_, err = brands.Delete(ctx, lock.ID)
	require.NoError(t, err)
	list, err := templates.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "ブランドA", list[0].Name)
	require.Len(t, list[0].Constraints, 2)
	assert.Equal(t, domain.BrandForbidden, list[0].Constraints[0].Type, "registration order")
	assert.Equal(t, "最安値\n激安", list[0].Constraints[0].Text)
	assert.Equal(t, domain.EnforceAdvisory, list[0].Constraints[1].Level)

	// Applying copies into the other Project as its own rows.
	require.NoError(t, templates.Apply(ctx, saved.ID, "p2"))
	got, err := brands.List(ctx, "p2")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "最安値\n激安", got[0].Text)
	assert.Equal(t, domain.EnforceLock, got[0].Level)
	assert.NotEqual(t, list[0].Constraints[0].ID, got[0].ID)

	// Applying again adds only what the Project lacks (here: after one copy was removed).
	require.NoError(t, templates.Apply(ctx, saved.ID, "p2"))
	got, err = brands.List(ctx, "p2")
	require.NoError(t, err)
	assert.Len(t, got, 2, "an equal constraint is not copied twice")
	_, err = brands.Delete(ctx, got[0].ID)
	require.NoError(t, err)
	require.NoError(t, templates.Apply(ctx, saved.ID, "p2"))
	got, err = brands.List(ctx, "p2")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// Editing the Project's copy leaves the template alone; deleting the template leaves the Project alone.
	_, err = brands.Delete(ctx, got[0].ID)
	require.NoError(t, err)
	list, err = templates.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list[0].Constraints, 2)
	require.NoError(t, templates.Delete(ctx, saved.ID))
	got, err = brands.List(ctx, "p2")
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.ErrorIs(t, templates.Delete(ctx, saved.ID), repository.ErrNotFound)
}

func TestBrandTemplateRepository_RejectsUnknownEmptyAndDuplicateNames(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedProject(t, db, "p1")
	seedProject(t, db, "empty")
	brands := repository.NewBrandRepository(db)
	templates := repository.NewBrandTemplateRepository(db)
	_, err := brands.Create(ctx, domain.BrandConstraint{ProjectID: "p1", Type: domain.BrandLogoRule, Level: domain.EnforceLock, Text: "ロゴを歪めない"})
	require.NoError(t, err)

	_, err = templates.SaveFromProject(ctx, "A", "no-such-project")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = templates.SaveFromProject(ctx, "A", "empty")
	assert.ErrorIs(t, err, repository.ErrEmpty)
	saved, err := templates.SaveFromProject(ctx, "A", "p1")
	require.NoError(t, err)
	_, err = templates.SaveFromProject(ctx, "a", "p1")
	assert.ErrorIs(t, err, repository.ErrConflict, "names are unique case-insensitively")
	_, err = templates.SaveFromProject(ctx, "Éclair", "p1")
	require.NoError(t, err)
	_, err = templates.SaveFromProject(ctx, "éclair", "p1")
	assert.ErrorIs(t, err, repository.ErrConflict, "names are unique by Unicode case folding, not for ASCII only")
	_, err = templates.SaveFromProject(ctx, "ДОМ", "p1")
	require.NoError(t, err)
	_, err = templates.SaveFromProject(ctx, "дом", "p1")
	assert.ErrorIs(t, err, repository.ErrConflict, "Cyrillic case variants are the same name")

	assert.ErrorIs(t, templates.Apply(ctx, "no-such-template", "p1"), repository.ErrTemplateNotFound)
	assert.ErrorIs(t, templates.Apply(ctx, saved.ID, "no-such-project"), repository.ErrNotFound)
}

func TestBrandTemplateRepository_ProjectAndEpisodeCreationCopyTheTemplateAtomically(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)
	brands := repository.NewBrandRepository(f.db)
	templates := repository.NewBrandTemplateRepository(f.db)
	_, err := brands.Create(ctx, domain.BrandConstraint{ProjectID: f.source, Type: domain.BrandPlatformRule, Level: domain.EnforceLock, Text: "9:16"})
	require.NoError(t, err)
	tpl, err := templates.SaveFromProject(ctx, "SNS", f.source)
	require.NoError(t, err)
	s := f.createFromSource(t)

	_, err = f.series.CreateEpisode(ctx, s.ID, "", tpl.ID, newEpisodeProject("ep2"))
	require.NoError(t, err)
	got, err := brands.List(ctx, "ep2")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "9:16", got[0].Text)

	// An unknown template creates nothing.
	_, err = f.series.CreateEpisode(ctx, s.ID, "", "ghost", newEpisodeProject("ep3"))
	assert.ErrorIs(t, err, repository.ErrTemplateNotFound)
	_, err = f.projects.Get(ctx, "ep3")
	assert.ErrorIs(t, err, repository.ErrNotFound)

	err = f.projects.Create(ctx, repository.NewProject{Project: newEpisodeProject("np1"), BrandTemplateID: tpl.ID})
	require.NoError(t, err)
	got, err = brands.List(ctx, "np1")
	require.NoError(t, err)
	assert.Len(t, got, 1)
	err = f.projects.Create(ctx, repository.NewProject{Project: newEpisodeProject("np2"), BrandTemplateID: "ghost"})
	assert.ErrorIs(t, err, repository.ErrTemplateNotFound)
	_, err = f.projects.Get(ctx, "np2")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}
