package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func ptr[T any](v T) *T { return &v }

func TestPolicyRepository_StoresTemplatePolicyLimitsAndBudget(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	id := createProject(t, repository.NewProjectRepository(db), domain.StatePlanning)
	repo := repository.NewPolicyRepository(db)

	got, err := repo.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.TemplateProductCM, got.Template)
	assert.Equal(t, domain.QualityBalanced, got.Quality)
	assert.Nil(t, got.ProjectBudgetCeiling)
	assert.Equal(t, domain.PolicyLimits{}, got.Limits, "a Project without overrides has none")

	require.NoError(t, repo.SetProductionTemplate(ctx, id, domain.TemplateDrama))
	require.NoError(t, repo.SetBudget(ctx, id, ptr(50.0), ptr(2.5)))
	require.NoError(t, repo.SetQualityPolicy(ctx, id, domain.QualityMaxQuality,
		&domain.PolicyLimits{MaxRetries: ptr(0), MaxGenerationSeconds: ptr(120), MinAcceptanceScore: ptr(90)}))

	got, err = repo.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.TemplateDrama, got.Template)
	assert.Equal(t, domain.QualityMaxQuality, got.Quality)
	assert.InDelta(t, 50, *got.ProjectBudgetCeiling, 1e-9)
	assert.InDelta(t, 2.5, *got.ShotBudgetCeiling, 1e-9)
	assert.Equal(t, 0, *got.Limits.MaxRetries, "an explicit 0 is stored as 0, not as unset")
	assert.Equal(t, 120, *got.Limits.MaxGenerationSeconds)
	assert.Nil(t, got.Limits.MinAxisScore)

	// Switching the policy without limits keeps the overrides; with limits it replaces them, clearing the nil ones.
	require.NoError(t, repo.SetQualityPolicy(ctx, id, domain.QualityFast, nil))
	got, _ = repo.Get(ctx, id)
	assert.Equal(t, domain.QualityFast, got.Quality)
	assert.Equal(t, 90, *got.Limits.MinAcceptanceScore)
	require.NoError(t, repo.SetQualityPolicy(ctx, id, domain.QualityFast, &domain.PolicyLimits{MinAxisScore: ptr(10)}))
	got, _ = repo.Get(ctx, id)
	assert.Nil(t, got.Limits.MaxRetries)
	assert.Equal(t, 10, *got.Limits.MinAxisScore)

	require.NoError(t, repo.SetBudget(ctx, id, nil, nil))
	got, _ = repo.Get(ctx, id)
	assert.Nil(t, got.ProjectBudgetCeiling, "an empty ceiling removes it")
	assert.Nil(t, got.ShotBudgetCeiling)
}

func TestPolicyRepository_UnknownProjectIsNotFoundAndLimitsAreBoundedByTheSchema(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	id := createProject(t, repository.NewProjectRepository(db), domain.StatePlanning)
	repo := repository.NewPolicyRepository(db)

	_, err := repo.Get(ctx, "missing")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	assert.ErrorIs(t, repo.SetProductionTemplate(ctx, "missing", domain.TemplateAnime), repository.ErrNotFound)
	assert.ErrorIs(t, repo.SetQualityPolicy(ctx, "missing", domain.QualityFast, &domain.PolicyLimits{}), repository.ErrNotFound)
	assert.ErrorIs(t, repo.SetBudget(ctx, "missing", nil, nil), repository.ErrNotFound)

	assert.Error(t, repo.SetQualityPolicy(ctx, id, domain.QualityFast, &domain.PolicyLimits{MinAcceptanceScore: ptr(101)}))
	assert.Error(t, repo.SetQualityPolicy(ctx, id, domain.QualityFast, &domain.PolicyLimits{MaxRetries: ptr(-1)}))
	assert.Error(t, repo.SetProductionTemplate(ctx, id, "movie"))
	got, _ := repo.Get(ctx, id)
	assert.Equal(t, domain.QualityBalanced, got.Quality, "a rejected write changes nothing")
}
