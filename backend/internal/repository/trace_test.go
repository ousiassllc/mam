package repository_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestTraceRepository_TakeTraceGathersEverythingThatLedToTheTake(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	spec := wideSpec("run")
	refs := []domain.Reference{{Kind: domain.RefCharacter, Name: "Mio", AssetID: "ref-1", Prompt: "portrait", Model: domain.ModelNanoBanana}}
	params, err := json.Marshal(struct {
		domain.TakeLineage
		Resolution string `json:"resolution"`
	}{domain.TakeLineage{WorldStateVersion: 4, References: refs}, "1080p"})
	require.NoError(t, err)
	pv, err := f.repo.SavePromptedSpec(ctx, f.shotID, spec, json.RawMessage(`{"requests":{}}`), nil)
	require.NoError(t, err)
	first, err := f.repo.CreateTake(ctx, repository.NewTake{
		ProjectID: f.projectID, ShotID: f.shotID, PromptVersionID: pv.ID, Model: domain.ModelKling3, Parameters: params,
		Asset: domain.Asset{ID: "a1", ProjectID: f.projectID, Type: domain.AssetVideo, FilePath: "p/a1.mp4", MimeType: "video/mp4", SizeBytes: 9},
	})
	require.NoError(t, err)
	judged, err := repository.NewJudgeRepository(f.db).Save(ctx, domain.JudgeResult{TakeID: first, Result: domain.JudgeFail, OverallConfidence: 0.5})
	require.NoError(t, err)
	repairs := repository.NewRepairRepository(f.db)
	action, err := repairs.CreateAction(ctx, judged.ID, domain.RepairFullRetake, domain.RepairInstructions{DecidedBy: domain.DecidedByRule, SourceTakeID: first})
	require.NoError(t, err)
	second, err := f.repo.CreateTake(ctx, repository.NewTake{
		ProjectID: f.projectID, ShotID: f.shotID, PromptVersionID: pv.ID, Model: domain.ModelSeedance2,
		Asset: domain.Asset{ID: "a2", ProjectID: f.projectID, Type: domain.AssetVideo, FilePath: "p/a2.mp4"},
	})
	require.NoError(t, err)
	require.NoError(t, repairs.SetResultingTake(ctx, action.ID, second))

	tr, err := repository.NewTraceRepository(f.db).TakeTrace(ctx, first)
	require.NoError(t, err)

	assert.Equal(t, f.projectID, tr.ProjectID)
	assert.Equal(t, f.shotID, tr.Take.ShotID)
	assert.Equal(t, 1, tr.PromptVersion.VersionNumber, "Prompt version")
	assert.Contains(t, string(tr.PromptVersion.SpecSnapshot), "run", "Source Shot Spec")
	assert.JSONEq(t, `{"requests":{}}`, string(tr.PromptVersion.AdapterOutput))
	assert.Equal(t, domain.ModelKling3, domain.ModelName(tr.Take.ModelName), "Model")
	assert.Contains(t, string(tr.ModelParameters), "1080p", "Model parameters")
	assert.Equal(t, 4, tr.WorldStateVersion, "World State version")
	assert.Equal(t, refs, tr.References, "References")
	require.NotNil(t, tr.Output, "Output")
	assert.Equal(t, "a1", tr.Output.AssetID)
	assert.EqualValues(t, 9, tr.Output.SizeBytes)
	require.Len(t, tr.Judgements, 1, "Judge result")
	assert.Equal(t, judged.ID, tr.Judgements[0].ID)
	require.Len(t, tr.Repairs, 1, "Repair history")
	assert.Equal(t, second, tr.Repairs[0].ResultingTakeID)
	assert.Nil(t, tr.ProducedBy, "the first Take was not made by a repair")

	// The repaired Take is traced back to the repair that made it. It predates the lineage record,
	// so its References fall back to the Reference Pack of its spec.
	tr2, err := repository.NewTraceRepository(f.db).TakeTrace(ctx, second)
	require.NoError(t, err)
	require.NotNil(t, tr2.ProducedBy)
	assert.Equal(t, action.ID, tr2.ProducedBy.ID)
	assert.Zero(t, tr2.WorldStateVersion)
	assert.Empty(t, tr2.References)

	_, err = repository.NewTraceRepository(f.db).TakeTrace(ctx, "missing")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestTraceRepository_ArchivedTakeHasNoOutput(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	pv, err := f.repo.SavePromptedSpec(ctx, f.shotID, wideSpec("run"), nil, nil)
	require.NoError(t, err)
	take, err := f.repo.CreateTake(ctx, repository.NewTake{
		ProjectID: f.projectID, ShotID: f.shotID, PromptVersionID: pv.ID, Model: domain.ModelKling3,
		Asset: domain.Asset{ID: "a1", ProjectID: f.projectID, Type: domain.AssetVideo, FilePath: "p/a1.mp4"},
	})
	require.NoError(t, err)
	require.NoError(t, exec(t, f.db, `DELETE FROM asset WHERE id = 'a1'`))
	require.NoError(t, exec(t, f.db, `UPDATE take SET file_path = NULL, status = 'rejected' WHERE id = ?`, take))

	tr, err := repository.NewTraceRepository(f.db).TakeTrace(ctx, take)

	require.NoError(t, err)
	assert.Nil(t, tr.Output)
	assert.Empty(t, tr.Take.FilePath)
}
