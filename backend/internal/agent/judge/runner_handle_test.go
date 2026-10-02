package judge_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

type silentChat struct{}

func (silentChat) Announce(context.Context, string, domain.ChatMessage) error { return nil }

func (silentChat) RaiseEscalation(context.Context, string, string, []string, string) error {
	return nil
}

// The client reads the Shot again when it is told that judging is done or failed: the Shot must be
// given back (not JUDGING) by then, or the card would spin until the page is reloaded.
func TestHandle_TheShotIsGivenBackBeforeDoneOrFailedIsAnnounced(t *testing.T) {
	tests := []struct {
		name     string
		frameErr error
		stage    generation.Stage
	}{
		{"judged", nil, generation.StageDone},
		{"failed", errors.New("ffprobe exploded"), generation.StageFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := repository.Open(ctx, filepath.Join(t.TempDir(), "mam.db"), nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
				for _, q := range []string{
					`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
					`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
					`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 4, 0)`,
					`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{"shot_objective":"A woman runs","camera":{"framing":"medium"}}')`,
					`INSERT INTO take (id, prompt_version_id, model_name, file_path) VALUES ('t1', 'pv1', 'kling-3.0', 'p1/a.mp4')`,
				} {
					if _, err := tx.ExecContext(ctx, q); err != nil {
						return err
					}
				}
				return nil
			}))
			assets := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(assets, "p1"), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(assets, "p1", "a.mp4"), []byte("video"), 0o600))

			rig := newRig(t, passAnswer, false, steady()...)
			rig.frames.err = tt.frameErr
			gens, judges := repository.NewGenerationRepository(db), repository.NewJudgeRepository(db)
			var (
				mu     sync.Mutex
				states = map[generation.Stage]domain.ShotState{}
			)
			runner := judge.NewRunner(judge.Deps{
				Generations: gens, Judges: judges, Chat: silentChat{}, Judge: rig.judge, AssetsDir: assets,
				Notify: func(p generation.Progress) {
					if p.Stage != generation.StageDone && p.Stage != generation.StageFailed {
						return
					}
					ref, err := gens.GetShot(context.Background(), p.ShotID)
					require.NoError(t, err)
					mu.Lock()
					states[p.Stage] = ref.Shot.State
					mu.Unlock()
				},
			})
			require.NoError(t, judges.BeginJudging(ctx, "sh1")) // what Start does

			raw, err := json.Marshal(map[string]string{"take_id": "t1", "shot_id": "sh1"})
			require.NoError(t, err)
			err = runner.Handle(ctx, domain.Job{ID: "j1", Kind: domain.JobJudge, Payload: raw})
			if tt.frameErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			mu.Lock()
			defer mu.Unlock()
			require.Contains(t, states, tt.stage)
			assert.Equal(t, domain.ShotPlanning, states[tt.stage], "the Shot is not JUDGING when the client is told")
		})
	}
}
