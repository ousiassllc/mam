package generation_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A run reads the Keyframe once however often it plans and however many Takes it makes (#1062): decide (a retry decides again), the
// clip of every Take and the estimate of Different Model share the read. A job is Start's own check of the budget and then the run.
// The file of the Keyframe is gone, so every read of it is logged.
func TestOrchestrator_ARunReadsTheKeyframeOnce(t *testing.T) {
	regenerate := func(r generation.Regeneration) func(*fixture) error {
		return func(f *fixture) error {
			_, err := f.orch.Regenerate(context.Background(), f.ref(t), r)
			return err
		}
	}
	for name, c := range map[string]struct {
		run   func(*fixture) error
		reads int
	}{
		"generate, three Takes": {func(f *fixture) error { return f.generate(t, "") }, 2},
		"candidates, retried after a failing verdict": {func(f *fixture) error {
			f.judge.verdicts = func(int) (domain.JudgeResult, error) { return domain.JudgeResult{Result: domain.JudgeFail}, nil }
			return f.candidates(t, "")
		}, 2},
		"full retake":             {regenerate(generation.Regeneration{Action: domain.RepairFullRetake}), 1},
		"different model":         {regenerate(generation.Regeneration{Action: domain.RepairDifferentModel, SourceModel: domain.ModelSeedance2}), 1},
		"reference reinforcement": {regenerate(generation.Regeneration{Action: domain.RepairReferenceReinforce}), 1},
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			f := newFixture(t, options{withJudge: true, logger: slog.New(slog.NewTextHandler(&logs, nil))})
			require.NoError(t, f.referencePack(t))
			require.NoError(t, f.generate(t, ""))
			f.removeKeyframeFile(t)
			logs.Reset()

			require.NoError(t, c.run(f))

			assert.Equal(t, c.reads, strings.Count(logs.String(), "reference image unavailable"))
		})
	}
}
