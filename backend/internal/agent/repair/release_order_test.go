package repair_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// terminalShotStates records the state of the Shot at the moment a done / failed notice goes out:
// the client reads the Shot again on it, so it must already be given back.
type terminalShotStates struct {
	mu     sync.Mutex
	states map[generation.Stage]domain.ShotState
}

func withTerminalShotStates(rec *terminalShotStates) fxOption {
	return func(f *fx, d *repair.Deps) {
		rec.states = map[generation.Stage]domain.ShotState{}
		inner := d.Notify
		d.Notify = func(p generation.Progress) {
			if p.Stage == generation.StageDone || p.Stage == generation.StageFailed {
				ref, err := f.gens.GetShot(context.Background(), p.ShotID)
				if err == nil {
					rec.mu.Lock()
					rec.states[p.Stage] = ref.Shot.State
					rec.mu.Unlock()
				}
			}
			inner(p)
		}
	}
}

func TestHandle_TheShotIsGivenBackBeforeDoneOrFailedIsAnnounced(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fx)
		stage generation.Stage
	}{
		{"a new Take was made", func(*fx) {}, generation.StageDone},
		{"the user is asked (Escalation)", func(f *fx) { f.usedRetries(t, 2) }, generation.StageDone},
		{"the repair failed", func(f *fx) { f.regen.regenErr = errProvider }, generation.StageFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rec terminalShotStates
			f := newFx(t, withTerminalShotStates(&rec))
			tt.setup(f)

			err := f.run(t, false)
			if tt.stage == generation.StageFailed {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			rec.mu.Lock()
			defer rec.mu.Unlock()
			require.Contains(t, rec.states, tt.stage)
			assert.Equal(t, domain.ShotPlanning, rec.states[tt.stage], "the Shot is not REPAIRING when the client is told")
		})
	}
}
