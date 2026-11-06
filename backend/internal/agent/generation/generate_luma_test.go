package generation_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// lumaFixture is a 10 second Shot made by Luma Ray 3.2, with a Keyframe when keyframe is set, whose file is deleted when removed is.
// Luma is the primary of the Quality Policy, or with routed the candidate Multi-model routing picks for its price (no Judge record
// or Minimum acceptance score stands in the way).
func lumaFixture(t *testing.T, keyframe, removed, routed bool) *fixture {
	luma := modeladapter.VideoTarget{Model: domain.ModelLumaRay32, Resolution: "720p"}
	zero, opt := 0, options{withJudge: true}
	opt.config = func(cfg *modeladapter.Config) {
		route := cfg.Policies[domain.QualityBalanced]
		if routed {
			cfg.Routing.MinSamples = 0
			cfg.Routing.Candidates[domain.QualityBalanced] = []modeladapter.VideoTarget{route.Primary, luma}
			return
		}
		route.Primary = luma
		cfg.Policies[domain.QualityBalanced] = route
	}
	if routed {
		opt.limits = &domain.PolicyLimits{MinAcceptanceScore: &zero}
	}
	f := newFixture(t, opt)
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

// reinforce is a Reference Reinforcement of the Shot.
func (f *fixture) reinforce(t *testing.T) error {
	t.Helper()
	_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{
		Action: domain.RepairReferenceReinforce, Issues: []domain.JudgeIssue{{Type: domain.IssueTypeFaceIdentityDrift, Severity: domain.SeverityHigh}},
	})
	return err
}

// A Luma Ray 3.2 clip is 5 seconds long only when its first frame is really attached (#902): a Keyframe whose file cannot be read
// leaves the clip as long as the Shot (10 seconds) in every way of generating it (generate, Candidates, a regeneration other than
// Different Model), and the estimate (RegenerationStrategy, the budget check of Start) is the cost of that clip. Reference
// Reinforcement is one of them: Luma takes no free references, so its Keyframe stays the first frame (#1057). It holds whether the
// Quality Policy or Multi-model routing chose Luma.
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
		for _, routed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, routed %v", c.name, routed), func(t *testing.T) {
				paths := map[string]func(*fixture) error{
					"generate":   func(f *fixture) error { return f.generate(t, "") },
					"candidates": func(f *fixture) error { return f.candidates(t, "") },
					"full retake": func(f *fixture) error {
						require.NoError(t, f.generate(t, ""))
						_, err := f.orch.Regenerate(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairFullRetake})
						return err
					},
					"reference reinforcement": func(f *fixture) error {
						require.NoError(t, f.generate(t, ""))
						return f.reinforce(t)
					},
				}
				for name, run := range paths {
					f := lumaFixture(t, c.keyframe, c.removed, routed)
					require.NoError(t, run(f), name)
					require.NotEmpty(t, f.video.sent(), name)
					for _, req := range f.video.sent() {
						assert.Equal(t, c.seconds, req.DurationSeconds, name)
						assert.Equal(t, c.keyframe && !c.removed, req.FirstFrame != nil, name)
						assert.Empty(t, req.References, name)
					}
				}
				f := lumaFixture(t, c.keyframe, c.removed, routed)
				require.NoError(t, f.generate(t, ""))
				s, err := f.orch.RegenerationStrategy(context.Background(), f.ref(t), generation.Regeneration{Action: domain.RepairFullRetake})
				require.NoError(t, err)
				assert.Equal(t, domain.ModelLumaRay32, s.Primary.Model)
				assert.Equal(t, routed, s.Routing.Mode == domain.RoutingAdaptive, "routing chose Luma, not the Quality Policy")
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
}

// The clip of a job is the length of the Shot, not the clip of the primary model: Prepare sizes it for each model, so the model that
// takes over from Luma Ray 3.2 (whose clip with a frame is 5 seconds) makes the 10 second clip the Shot asks for (#1057).
func TestOrchestrator_AModelThatTakesOverFromLumaMakesTheClipOfTheShot(t *testing.T) {
	f := lumaFixture(t, true, false, false) // the alternative of the Quality Policy is Kling 3.0
	require.NoError(t, f.generate(t, ""))
	before := f.video.callCount()
	f.video.failOn = func(n int) error { // Luma times out, Kling takes over
		if n == before+1 {
			return modeladapter.ErrTimeout
		}
		return nil
	}

	require.NoError(t, f.reinforce(t))

	sent := f.video.sent()[before:]
	require.Len(t, sent, 2)
	assert.Equal(t, 5.0, sent[0].DurationSeconds, "Luma: a clip with a frame")
	assert.Equal(t, 10.0, sent[1].DurationSeconds, "Kling: the Shot's length")
	assert.NotNil(t, sent[1].FirstFrame)
}
