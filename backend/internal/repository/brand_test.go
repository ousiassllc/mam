package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestBrandRepository_CreateListDeleteKeepsRegistrationOrderAndScopesToTheProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedProject(t, db, "p1")
	seedProject(t, db, "p2")
	repo := repository.NewBrandRepository(db)

	for _, typ := range domain.BrandConstraintTypes() {
		c, err := repo.Create(ctx, domain.BrandConstraint{ProjectID: "p1", Type: typ, Level: domain.EnforceLock, Text: "rule " + string(typ)})
		require.NoError(t, err, typ)
		assert.NotEmpty(t, c.ID)
	}
	other, err := repo.Create(ctx, domain.BrandConstraint{ProjectID: "p2", Type: domain.BrandForbidden, Level: domain.EnforceAdvisory, Text: "a\nb"})
	require.NoError(t, err)

	list, err := repo.List(ctx, "p1")
	require.NoError(t, err)
	require.Len(t, list, 8)
	for i, typ := range domain.BrandConstraintTypes() {
		assert.Equal(t, typ, list[i].Type, "registration order")
		assert.Equal(t, "rule "+string(typ), list[i].Text)
	}
	got, err := repo.List(ctx, "p2")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "a\nb", got[0].Text)
	assert.Equal(t, domain.EnforceAdvisory, got[0].Level)

	pid, err := repo.Delete(ctx, other.ID)
	require.NoError(t, err)
	assert.Equal(t, "p2", pid)
	got, err = repo.List(ctx, "p2")
	require.NoError(t, err)
	assert.Empty(t, got)
	_, err = repo.Delete(ctx, other.ID)
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestBrandRepository_UnknownProjectIsNotFoundAndTheSchemaRejectsBadEnums(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedProject(t, db, "p1")
	repo := repository.NewBrandRepository(db)

	_, err := repo.Create(ctx, domain.BrandConstraint{ProjectID: "nope", Type: domain.BrandLogoRule, Level: domain.EnforceLock, Text: "x"})
	assert.ErrorIs(t, err, repository.ErrNotFound)

	assert.Error(t, exec(t, db, `INSERT INTO brand_constraint (id, project_id, type, enforcement_level, value) VALUES ('x', 'p1', 'tone', 'lock', '{}')`))
	assert.Error(t, exec(t, db, `INSERT INTO brand_constraint (id, project_id, type, enforcement_level, value) VALUES ('x', 'p1', 'logo_rule', 'strict', '{}')`))
}

func TestBrandRepository_ConstraintsAreDeletedWithTheirProject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	seedProject(t, db, "p1")
	repo := repository.NewBrandRepository(db)
	_, err := repo.Create(ctx, domain.BrandConstraint{ProjectID: "p1", Type: domain.BrandLogoRule, Level: domain.EnforceLock, Text: "x"})
	require.NoError(t, err)

	require.NoError(t, exec(t, db, `DELETE FROM project WHERE id = 'p1'`))

	list, err := repo.List(ctx, "p1")
	require.NoError(t, err)
	assert.Empty(t, list)
}
