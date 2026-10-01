package judge

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// scoresOf gives every axis of the product_cm template the same score.
func scoresOf(s float64) map[domain.EvaluationAxis]float64 {
	m := map[domain.EvaluationAxis]float64{}
	for _, a := range domain.TemplateProductCM.Axes() {
		m[a] = s
	}
	return m
}

// pickRig is a Runner over a database with an AUTO Project (balanced policy: Minimum acceptance
// score 75) and a Shot with two Takes, t1 and t2.
func pickRig(t *testing.T) (*Runner, repository.TakeRef) {
	t.Helper()
	ctx := context.Background()
	db, err := repository.Open(ctx, filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
			`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 4, 0)`,
			`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{"shot_objective":"run"}')`,
			`INSERT INTO take (id, prompt_version_id, model_name, file_path, created_at) VALUES ('t1', 'pv1', 'kling-3.0', 'p1/a.mp4', '2026-01-01T00:00:01.000Z')`,
			`INSERT INTO take (id, prompt_version_id, model_name, file_path, created_at) VALUES ('t2', 'pv1', 'kling-3.0', 'p1/b.mp4', '2026-01-01T00:00:02.000Z')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	r := NewRunner(Deps{
		Generations: repository.NewGenerationRepository(db), Judges: repository.NewJudgeRepository(db),
		Policies: repository.NewPolicyRepository(db), Takes: repository.NewTakeRepository(db), Projects: repository.NewProjectRepository(db),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ref, err := r.Judges.GetTake(ctx, "t1")
	require.NoError(t, err)
	return r, ref
}

func (r *Runner) saveJudged(t *testing.T, takeID string, res domain.JudgeResult) {
	t.Helper()
	res.TakeID = takeID
	_, err := r.Judges.Save(context.Background(), res)
	require.NoError(t, err)
}

// selectedTake is the Take the Shot uses (shot.selected_take_id).
func (r *Runner) selectedTake(t *testing.T) string {
	t.Helper()
	takes, err := r.Takes.ShotTakes(context.Background(), "sh1")
	require.NoError(t, err)
	for _, tk := range takes {
		if tk.Adopted {
			return tk.ID
		}
	}
	return ""
}

func TestPick_AutoDoesNotAdoptATakeBelowTheMinimumAcceptanceScore(t *testing.T) {
	r, ref := pickRig(t)
	// The Judge passed t1, but its scores are under the policy minimum: the Repair loop rejects it.
	r.saveJudged(t, "t1", domain.JudgeResult{Result: domain.JudgePass, Scores: scoresOf(60)})

	line := r.pick(context.Background(), ref)

	assert.Contains(t, line, "おすすめ", "the Take is only recommended")
	assert.Empty(t, r.selectedTake(t), "AUTO adopts only a Take the Retry Policy accepts")
}

func TestPick_AutoAdoptsAnAcceptedTakeEvenWhenAnotherPassedWithBetterIssues(t *testing.T) {
	r, ref := pickRig(t)
	r.saveJudged(t, "t1", domain.JudgeResult{Result: domain.JudgePass, Scores: scoresOf(60)})
	r.saveJudged(t, "t2", domain.JudgeResult{Result: domain.JudgePass, Scores: scoresOf(90), Issues: []domain.JudgeIssue{{Type: "x", Severity: domain.SeverityLow, Dimension: domain.DimActing}}})

	line := r.pick(context.Background(), ref)

	assert.Contains(t, line, "採用")
	assert.Equal(t, "t2", r.selectedTake(t))
}

func TestPick_AutoAdoptsAPassingTakeThatReachesTheMinimum(t *testing.T) {
	r, ref := pickRig(t)
	r.saveJudged(t, "t1", domain.JudgeResult{Result: domain.JudgePass, Scores: scoresOf(80)})

	r.pick(context.Background(), ref)

	assert.Equal(t, "t1", r.selectedTake(t))
}
