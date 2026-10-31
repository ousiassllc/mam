package generation_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Different Model estimates the clip as Prepare makes it (#886). A Luma Ray 3.2 clip is 5 seconds long only when its first frame is
// really attached: a Keyframe whose file cannot be read leaves the clip as long as the Shot (10 seconds), and the estimate follows
// the clip, so the cost is never lower than what is made. The request sent is the one that was estimated.
func TestOrchestrator_DifferentModelEstimatesALumaClipAsPrepareMakesIt(t *testing.T) {
	cases := []struct {
		name               string
		keyframe, readable bool
		seconds, cost      float64
	}{
		{"a Keyframe that is read makes a 5 second clip", true, true, 5, 0.3},
		{"a Keyframe that cannot be read leaves the clip 10 seconds long", true, false, 10, 0.9},
		{"a Shot without a Keyframe makes a 10 second clip", false, false, 10, 0.9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, options{config: func(cfg *modeladapter.Config) {
				route := cfg.Policies[domain.QualityBalanced]
				route.Fallback = modeladapter.VideoTarget{Model: domain.ModelLumaRay32, Resolution: "720p"}
				cfg.Policies[domain.QualityBalanced] = route
			}})
			require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `UPDATE shot SET duration_seconds = 10 WHERE id = ?`, f.shotID)
				return err
			}))
			if c.keyframe {
				require.NoError(t, f.referencePack(t))
			}
			require.NoError(t, f.generate(t, ""))
			if c.keyframe && !c.readable {
				f.removeKeyframeFile(t)
			}
			regen := generation.Regeneration{Action: domain.RepairDifferentModel, SourceModel: domain.ModelSeedance2}

			s, err := f.orch.RegenerationStrategy(context.Background(), f.ref(t), regen)

			require.NoError(t, err)
			assert.Equal(t, domain.ModelLumaRay32, s.Primary.Model)
			assert.Equal(t, c.seconds, s.DurationSeconds)
			assert.InDelta(t, c.cost, s.CostPerTake, 1e-9)
			before := f.video.callCount()
			_, err = f.orch.Regenerate(context.Background(), f.ref(t), regen)
			require.NoError(t, err)
			sent := f.video.sent()
			require.Len(t, sent, before+1)
			assert.Equal(t, s.DurationSeconds, sent[before].DurationSeconds, "the clip is as long as estimated")
			assert.Equal(t, c.readable, sent[before].FirstFrame != nil)
		})
	}
}

// removeKeyframeFile deletes the file of the Keyframe of the Shot, which stays a Reference of it.
func (f *fixture) removeKeyframeFile(t *testing.T) {
	t.Helper()
	for _, r := range f.ref(t).Shot.References() {
		if r.Kind == domain.RefKeyframe {
			asset, err := repository.NewAssetRepository(f.db).Get(context.Background(), f.projectID, r.AssetID)
			require.NoError(t, err)
			require.NoError(t, os.Remove(filepath.Join(f.assetsDir, asset.FilePath)))
		}
	}
}
