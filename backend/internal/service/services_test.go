package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// noFrames is a judge.FrameSource that is never asked: the wiring only checks that it is set.
type noFrames struct{}

func (noFrames) Sample(context.Context, string, judge.SampleSpec) (judge.Clip, error) {
	return judge.Clip{}, errors.New("not used")
}

// TestNew_RegistersJobKindsByDeps pins which Deps switch which job handlers on: the plan job needs
// the queue and the LLM, the judge job also Frames, the generate job also the Model Adapter, and
// the repair job generation and the Judge together.
func TestNew_RegistersJobKindsByDeps(t *testing.T) {
	tests := []struct {
		name   string
		tweak  func(*service.Deps)
		expect []domain.JobKind
	}{
		{"every dependency", func(*service.Deps) {},
			[]domain.JobKind{domain.JobPlan, domain.JobJudge, domain.JobGenerate, domain.JobRepair}},
		{"no LLM", func(d *service.Deps) { d.LLM = nil }, nil},
		{"no Frames", func(d *service.Deps) { d.Frames = nil },
			[]domain.JobKind{domain.JobPlan, domain.JobGenerate}},
		{"no Models", func(d *service.Deps) { d.Models = nil },
			[]domain.JobKind{domain.JobPlan, domain.JobJudge}},
		{"no Frames and no Models", func(d *service.Deps) { d.Frames, d.Models = nil, nil },
			[]domain.JobKind{domain.JobPlan}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
			deps := service.Deps{
				DB: db, Jobs: jobs, LLM: &genReasoner{}, Frames: noFrames{}, AssetsDir: t.TempDir(),
				Models: modeladapter.New(modeladapter.DefaultConfig(), modeladapter.Backends{}),
			}
			tc.tweak(&deps)

			svc := service.New(deps)
			t.Cleanup(svc.Close)

			for _, kind := range domain.JobKinds() {
				assert.Equal(t, slices.Contains(tc.expect, kind), jobs.Handles(kind), "job kind %q", kind)
			}
		})
	}
}

// TestNew_NoJobsWiresNoAgent: without a queue no agent is wired (the Judge is unavailable), and New still builds.
func TestNew_NoJobsWiresNoAgent(t *testing.T) {
	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	svc := service.New(service.Deps{DB: db, LLM: &genReasoner{}, Frames: noFrames{}, AssetsDir: t.TempDir()})
	t.Cleanup(svc.Close)

	_, err = svc.Judge.Judge(context.Background(), "missing")
	assert.ErrorIs(t, err, service.ErrUnavailable)
}
