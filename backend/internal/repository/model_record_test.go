package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func TestGenerationRepository_ModelRecordsAveragesTheLatestJudgeRunOfEachTakePerModel(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	pv, err := f.repo.SavePromptedSpec(ctx, f.shotID, wideSpec("run"), nil)
	require.NoError(t, err)
	judges := repository.NewJudgeRepository(f.db)
	n := 0
	take := func(model domain.ModelName, verdicts ...domain.JudgeVerdict) string {
		n++
		id, err := f.repo.CreateTake(ctx, repository.NewTake{
			ProjectID: f.projectID, ShotID: f.shotID, PromptVersionID: pv.ID, Model: model,
			Asset: domain.Asset{ID: fmt.Sprintf("a%d", n), ProjectID: f.projectID, Type: domain.AssetVideo, FilePath: fmt.Sprintf("p/a%d.mp4", n)},
		})
		require.NoError(t, err)
		for _, v := range verdicts {
			_, err := judges.Save(ctx, domain.JudgeResult{TakeID: id, Result: v, OverallConfidence: 0.9})
			require.NoError(t, err)
		}
		return id
	}
	score := func(id string, s string) {
		require.NoError(t, f.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE take SET scores = ? WHERE id = ?`, s, id)
			return err
		}))
	}

	pass := take(domain.ModelKling3, domain.JudgePass)
	score(pass, `{"product":100,"acting":80,"camera":60,"continuity":100}`)
	rejudged := take(domain.ModelKling3, domain.JudgeFail, domain.JudgePass) // the newest run counts: pass
	score(rejudged, `{"product":80,"acting":60,"camera":40,"continuity":80}`)
	failed := take(domain.ModelKling3, domain.JudgeFail)
	score(failed, `{"product":0,"acting":0,"camera":0,"continuity":0}`)
	other := take(domain.ModelSeedance2, domain.JudgePass)
	score(other, `{"product":90,"acting":90,"camera":90,"continuity":90}`)
	take(domain.ModelRunwayGen45) // never judged: no record
	unscored := take(domain.ModelFlux2, domain.JudgePass)
	// A judged Take without scores is left out.
	require.NoError(t, f.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE take SET scores = NULL WHERE id = ?`, unscored)
		return err
	}))

	got, err := f.repo.ModelRecords(ctx, f.projectID)
	require.NoError(t, err)

	require.Len(t, got, 2, "only models with judged and scored Takes have a record")
	kling := got[domain.ModelKling3]
	assert.Equal(t, 3, kling.Samples)
	assert.InDelta(t, 60, kling.Scores.Product, 1e-9)
	assert.InDelta(t, 46.666, kling.Scores.Acting, 1e-2)
	assert.InDelta(t, 2.0/3, kling.PassRate, 1e-9)
	assert.Equal(t, 1, got[domain.ModelSeedance2].Samples)
	assert.InDelta(t, 1.0, got[domain.ModelSeedance2].PassRate, 1e-9)

	other2 := createProject(t, repository.NewProjectRepository(f.db), domain.StatePlanning)
	empty, err := f.repo.ModelRecords(ctx, other2)
	require.NoError(t, err)
	assert.Empty(t, empty, "the record is per Project")
}
