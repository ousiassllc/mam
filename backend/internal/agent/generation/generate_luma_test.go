package generation_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// lumaFixture is a 10 second Shot made by Luma Ray 3.2, with a Keyframe when keyframe is set, whose file is deleted when removed is.
func lumaFixture(t *testing.T, keyframe, removed bool) *fixture {
	f := newFixture(t, options{withJudge: true, config: func(cfg *modeladapter.Config) {
		route := cfg.Policies[domain.QualityBalanced]
		route.Primary = modeladapter.VideoTarget{Model: domain.ModelLumaRay32, Resolution: "720p"}
		cfg.Policies[domain.QualityBalanced] = route
	}})
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE shot SET duration_seconds = 10 WHERE id = ?`, f.shotID)
		return err
	}))
	if keyframe {
		require.NoError(t, f.referencePack(t))
	}
	if removed {
		f.removeKeyframeFile(t)
	}
	return f
}

// A Luma Ray 3.2 clip is 5 seconds long only when its first frame is really attached (#902): a Keyframe whose file cannot be read
// leaves the clip as long as the Shot (10 seconds) in every way of generating it (generate, Candidates, a regeneration other than
// Different Model), and the estimate (RegenerationStrategy, the budget check of Start) is the cost of that clip.
func TestOrchestrator_LumaClipFollowsTheFirstFrameThatIsAttached(t *testing.T) {
	cases := []struct {
		name              string
		keyframe, removed bool
		seconds, cost     float64
	}{
		{"a Keyframe that is read", true, false, 5, 0.3},
		{"a Keyframe that cannot be read", true, true, 10, 0.9},
		{"no Keyframe", false, false, 10, 0.9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			paths := map[string]func(*fixture) error{
				"generate":   func(f *fixture) error { return f.generate(t, "") },
				"candidates": func(f *fixture) error { return f.candidates(t, "") },
				"full retake": func(f *fixture) error {
					require.NoError(t, f.generate(t, ""))
					_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairFullRetake})
					return err
				},
			}
			for name, run := range paths {
				f := lumaFixture(t, c.keyframe, c.removed)
				require.NoError(t, run(f), name)
				require.NotEmpty(t, f.video.sent(), name)
				for _, req := range f.video.sent() {
					assert.Equal(t, c.seconds, req.DurationSeconds, name)
					assert.Equal(t, c.keyframe && !c.removed, req.FirstFrame != nil, name)
				}
			}
			f := lumaFixture(t, c.keyframe, c.removed)
			require.NoError(t, f.generate(t, ""))
			s, err := f.orch.RegenerationStrategy(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairFullRetake})
			require.NoError(t, err)
			assert.Equal(t, c.seconds, s.DurationSeconds)
			assert.InDelta(t, c.cost, s.CostPerTake, 1e-9)

			// A ceiling 0.5 USD above what is spent holds the 5 second clip (0.30 USD) but not the 10 second one (0.90 USD).
			spent, err := f.gens.Budget(context.Background(), f.projectID, f.shotID)
			require.NoError(t, err)
			require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `UPDATE project SET project_budget_ceiling = ? WHERE id = ?`, spent.ProjectSpent+0.5, f.projectID)
				return err
			}))
			_, err = f.orch.Start(context.Background(), f.shotID, "")
			assert.Equal(t, c.seconds == 5, err == nil, "ErrBudgetExceeded only for the 10 second clip: %v", err)
			assert.Equal(t, c.seconds == 10, errors.Is(err, generation.ErrBudgetExceeded))
		})
	}
}
