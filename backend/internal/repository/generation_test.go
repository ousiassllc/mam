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

type genRepoFixture struct {
	db        *repository.DB
	repo      *repository.GenerationRepository
	projectID string
	shotID    string
}

func newGenRepoFixture(t *testing.T) genRepoFixture {
	t.Helper()
	ctx := context.Background()
	db := openTestDB(t)
	projects := repository.NewProjectRepository(db)
	plans := repository.NewPlanRepository(db)
	id := createProject(t, projects, domain.StatePlanning)
	require.NoError(t, plans.SavePlan(ctx, id,
		domain.ScenePlan{Scenes: []domain.PlannedScene{{Title: "A", Shots: []domain.PlannedShot{{Objective: "one", DurationSeconds: 3}, {Objective: "two", DurationSeconds: 2}}}}},
		domain.Storyboard{Shots: [][]domain.ShotStoryboard{{{Description: "x"}, {Description: "y"}}}}))
	scenes, err := plans.ListScenes(ctx, id)
	require.NoError(t, err)
	shots, err := plans.ListShots(ctx, scenes[0].ID)
	require.NoError(t, err)
	return genRepoFixture{db: db, repo: repository.NewGenerationRepository(db), projectID: id, shotID: shots[0].ID}
}

func TestGenerationRepository_StartShotClaimsAShotOnce(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)

	ok, err := f.repo.StartShot(ctx, f.shotID)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = f.repo.StartShot(ctx, f.shotID)
	require.NoError(t, err)
	assert.False(t, ok, "a second claim is refused while the Shot is GENERATING")

	require.NoError(t, f.repo.SetShotState(ctx, f.shotID, domain.ShotPlanning))
	ok, _ = f.repo.StartShot(ctx, f.shotID)
	assert.True(t, ok)
	assert.ErrorIs(t, f.repo.SetShotState(ctx, "missing", domain.ShotPlanning), repository.ErrNotFound)
}

func TestGenerationRepository_ResetStaleShotsReleasesOnlyGeneratingShots(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	_, err := f.repo.StartShot(ctx, f.shotID)
	require.NoError(t, err)

	n, err := f.repo.ResetStaleShots(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	ref, err := f.repo.GetShot(ctx, f.shotID)
	require.NoError(t, err)
	assert.Equal(t, domain.ShotPlanning, ref.Shot.State)
	assert.Equal(t, f.projectID, ref.ProjectID)
	assert.Equal(t, "A", ref.Scene.Title)

	n, _ = f.repo.ResetStaleShots(ctx)
	assert.Zero(t, n)
	_, err = f.repo.GetShot(ctx, "missing")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestGenerationRepository_ListProjectShotsIsInStoryOrder(t *testing.T) {
	f := newGenRepoFixture(t)
	shots, err := f.repo.ListProjectShots(context.Background(), f.projectID)
	require.NoError(t, err)
	require.Len(t, shots, 2)
	assert.Equal(t, "one", shots[0].Shot.Objective)
	assert.Equal(t, "two", shots[1].Shot.Objective)
}

func TestGenerationRepository_SavePromptedSpecKeepsStoryboardAndVersionsEverySave(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	spec := domain.ShotSpec{ShotObjective: "A woman runs", Camera: domain.SpecCamera{Framing: "wide"}}

	pv1, err := f.repo.SavePromptedSpec(ctx, f.shotID, spec, json.RawMessage(`{"n":1}`), nil)
	require.NoError(t, err)
	spec.ShotObjective = "A woman walks"
	pv2, err := f.repo.SavePromptedSpec(ctx, f.shotID, spec, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, pv1.VersionNumber)
	assert.Equal(t, 2, pv2.VersionNumber)

	ref, err := f.repo.GetShot(ctx, f.shotID)
	require.NoError(t, err)
	stored, ok := ref.Shot.CanonicalSpec()
	require.True(t, ok)
	assert.Equal(t, "A woman walks", stored.ShotObjective)
	_, hasBoard := ref.Shot.Storyboard()
	assert.True(t, hasBoard, "the storyboard written by the planning stage survives")

	var snapshot string
	var output sql.NullString
	require.NoError(t, f.db.Reader().QueryRow(`SELECT shot_spec_snapshot, model_adapter_output FROM prompt_version WHERE id = ?`, pv1.ID).Scan(&snapshot, &output))
	assert.Contains(t, snapshot, "A woman runs", "the snapshot is what the spec was at the time")
	assert.JSONEq(t, `{"n":1}`, output.String)
	require.NoError(t, f.db.Reader().QueryRow(`SELECT model_adapter_output FROM prompt_version WHERE id = ?`, pv2.ID).Scan(&output))
	assert.False(t, output.Valid)

	_, err = f.repo.SavePromptedSpec(ctx, "missing", spec, nil, nil)
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestGenerationRepository_CreateTakeStoresTakeAssetAndCostTogether(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	pv, err := f.repo.SavePromptedSpec(ctx, f.shotID, domain.ShotSpec{ShotObjective: "x", Camera: domain.SpecCamera{Framing: "wide"}}, nil, nil)
	require.NoError(t, err)

	id, err := f.repo.CreateTake(ctx, repository.NewTake{
		ProjectID: f.projectID, ShotID: f.shotID, PromptVersionID: pv.ID, Model: domain.ModelSeedance2,
		Asset: domain.Asset{ID: "asset-1", ProjectID: f.projectID, Type: domain.AssetVideo, FilePath: "p/asset-1.mp4", MimeType: "video/mp4", SizeBytes: 3}, EstimatedCost: 0.32,
	})
	require.NoError(t, err)

	var model, status, path string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT model_name, status, file_path FROM take WHERE id = ?`, id).Scan(&model, &status, &path))
	assert.Equal(t, "seedance-2.0", model)
	assert.Equal(t, "candidate", status)
	assert.Equal(t, "p/asset-1.mp4", path)
	var provider string
	var cost float64
	require.NoError(t, f.db.Reader().QueryRow(`SELECT provider, estimated_cost FROM cost_ledger_entry WHERE take_id = ? AND shot_id = ?`, id, f.shotID).Scan(&provider, &cost))
	assert.Equal(t, "seedance-2.0", provider)
	assert.InDelta(t, 0.32, cost, 1e-9)

	// A failing insert (unknown prompt version) leaves neither the asset nor the cost behind.
	_, err = f.repo.CreateTake(ctx, repository.NewTake{
		ProjectID: f.projectID, ShotID: f.shotID, PromptVersionID: "missing", Model: domain.ModelKling3,
		Asset: domain.Asset{ID: "asset-2", ProjectID: f.projectID, Type: domain.AssetVideo, FilePath: "p/asset-2.mp4"}, EstimatedCost: 1,
	})
	require.Error(t, err)
	var n int
	require.NoError(t, f.db.Reader().QueryRow(`SELECT (SELECT COUNT(*) FROM asset WHERE id = 'asset-2') + (SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'kling-3.0')`).Scan(&n))
	assert.Zero(t, n)
}

func TestGenerationRepository_CandidateSpecsAreVersionedAndOnlyTheAdoptedOneBecomesTheShotSpec(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	_, err := f.repo.SavePromptedSpec(ctx, f.shotID, domain.ShotSpec{ShotObjective: "current", Camera: domain.SpecCamera{Framing: "wide"}}, nil, nil)
	require.NoError(t, err)

	pv, err := f.repo.SaveCandidateSpec(ctx, f.shotID, domain.ShotSpec{ShotObjective: "acting first", Camera: domain.SpecCamera{Framing: "close"}}, json.RawMessage(`{"candidate_strategy":"acting_first"}`), nil)
	require.NoError(t, err)
	assert.Equal(t, 2, pv.VersionNumber)
	ref, err := f.repo.GetShot(ctx, f.shotID)
	require.NoError(t, err)
	stored, _ := ref.Shot.CanonicalSpec()
	assert.Equal(t, "current", stored.ShotObjective, "a Candidate is only an alternative")

	require.NoError(t, f.repo.AdoptSpec(ctx, f.shotID, domain.ShotSpec{ShotObjective: "acting first", Camera: domain.SpecCamera{Framing: "close"}}))
	ref, err = f.repo.GetShot(ctx, f.shotID)
	require.NoError(t, err)
	stored, _ = ref.Shot.CanonicalSpec()
	assert.Equal(t, "acting first", stored.ShotObjective)
	_, hasBoard := ref.Shot.Storyboard()
	assert.True(t, hasBoard, "the storyboard survives")
	assert.ErrorIs(t, f.repo.AdoptSpec(ctx, "missing", stored), repository.ErrNotFound)
}

func TestGenerationRepository_MarkAIPickMovesTheAIPickAndLeavesTheUsersChoiceAlone(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	pv, err := f.repo.SavePromptedSpec(ctx, f.shotID, domain.ShotSpec{ShotObjective: "x", Camera: domain.SpecCamera{Framing: "wide"}}, nil, nil)
	require.NoError(t, err)
	take := func(n string) string {
		id, err := f.repo.CreateTake(ctx, repository.NewTake{
			ProjectID: f.projectID, ShotID: f.shotID, PromptVersionID: pv.ID, Model: domain.ModelSeedance2,
			Asset: domain.Asset{ID: "asset-" + n, ProjectID: f.projectID, Type: domain.AssetVideo, FilePath: "p/" + n + ".mp4", MimeType: "video/mp4", SizeBytes: 1},
		})
		require.NoError(t, err)
		return id
	}
	status := func(id string) string {
		var s string
		require.NoError(t, f.db.Reader().QueryRow(`SELECT status FROM take WHERE id = ?`, id).Scan(&s))
		return s
	}
	a, b, selected := take("a"), take("b"), take("c")
	require.NoError(t, f.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE take SET status = 'selected' WHERE id = ?`, selected)
		return err
	}))

	require.NoError(t, f.repo.MarkAIPick(ctx, f.shotID, a))
	assert.Equal(t, "ai_pick", status(a))
	require.NoError(t, f.repo.MarkAIPick(ctx, f.shotID, b))
	assert.Equal(t, "candidate", status(a), "the earlier AI pick goes back to candidate")
	assert.Equal(t, "ai_pick", status(b))

	assert.ErrorIs(t, f.repo.MarkAIPick(ctx, f.shotID, selected), repository.ErrNotFound, "the user's choice is not overridden")
	assert.Equal(t, "selected", status(selected))
	assert.ErrorIs(t, f.repo.MarkAIPick(ctx, "other-shot", b), repository.ErrNotFound)
}

func TestGenerationRepository_SaveReferencePackReplacesThePackAndKeepsTheSpec(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	_, err := f.repo.SavePromptedSpec(ctx, f.shotID, domain.ShotSpec{ShotObjective: "x", Camera: domain.SpecCamera{Framing: "wide"}}, nil, nil)
	require.NoError(t, err)

	pack := func(assetID string) repository.NewReferencePack {
		return repository.NewReferencePack{
			ProjectID: f.projectID, ShotID: f.shotID,
			References: []domain.Reference{{Kind: domain.RefKeyframe, AssetID: assetID, Prompt: "p", Model: domain.ModelNanoBanana}},
			Assets:     []domain.Asset{{ID: assetID, ProjectID: f.projectID, Type: domain.AssetReference, FilePath: "p/" + assetID + ".png", MimeType: "image/png"}},
			Costs:      []domain.CostEntry{{ProjectID: f.projectID, ShotID: f.shotID, Provider: domain.ModelNanoBanana, EstimatedCost: 0.04}},
		}
	}
	require.NoError(t, f.repo.SaveReferencePack(ctx, pack("r1")))
	require.NoError(t, f.repo.SaveReferencePack(ctx, pack("r2")))

	ref, err := f.repo.GetShot(ctx, f.shotID)
	require.NoError(t, err)
	refs := ref.Shot.References()
	require.Len(t, refs, 1)
	assert.Equal(t, "r2", refs[0].AssetID, "the newer pack replaces the older one")
	_, ok := ref.Shot.CanonicalSpec()
	assert.True(t, ok, "the Canonical Shot Spec is untouched")
	var n int
	require.NoError(t, f.db.Reader().QueryRow(`SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'nano-banana'`).Scan(&n))
	assert.Equal(t, 2, n, "both packs were paid for")

	assert.ErrorIs(t, f.repo.SaveReferencePack(ctx, repository.NewReferencePack{ShotID: "missing"}), repository.ErrNotFound)
}

func TestGenerationRepository_BudgetSumsTheLedgerPerProjectAndShot(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	b, err := f.repo.Budget(ctx, f.projectID, f.shotID)
	require.NoError(t, err)
	assert.Nil(t, b.ProjectCeiling)
	assert.Nil(t, b.ShotCeiling)
	assert.Zero(t, b.ProjectSpent)

	require.NoError(t, f.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE project SET project_budget_ceiling = 10, shot_budget_ceiling = 2 WHERE id = ?`, f.projectID); err != nil {
			return err
		}
		for _, c := range []struct {
			shot   any
			est    float64
			actual any
		}{{f.shotID, 0.5, nil}, {f.shotID, 0.5, 0.7}, {nil, 1.0, nil}} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO cost_ledger_entry (id, project_id, shot_id, provider, estimated_cost, actual_cost) VALUES (lower(hex(randomblob(8))), ?, ?, 'kling-3.0', ?, ?)`,
				f.projectID, c.shot, c.est, c.actual); err != nil {
				return err
			}
		}
		return nil
	}))
	b, err = f.repo.Budget(ctx, f.projectID, f.shotID)
	require.NoError(t, err)
	require.NotNil(t, b.ProjectCeiling)
	assert.InDelta(t, 10, *b.ProjectCeiling, 1e-9)
	assert.InDelta(t, 2, *b.ShotCeiling, 1e-9)
	assert.InDelta(t, 2.2, b.ProjectSpent, 1e-9, "the actual cost replaces the estimate when it is known")
	assert.InDelta(t, 1.2, b.ShotSpent, 1e-9)

	_, err = f.repo.Budget(ctx, "missing", f.shotID)
	assert.ErrorIs(t, err, repository.ErrNotFound)
}
