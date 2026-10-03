package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func shotReader(state domain.ShotState, during func()) func(context.Context, string) (repository.ShotRef, error) {
	return func(_ context.Context, id string) (repository.ShotRef, error) {
		if during != nil {
			during()
		}
		return repository.ShotRef{Shot: domain.Shot{ID: id, State: state}}, nil
	}
}

func drain(ch <-chan generation.Progress) []generation.Stage {
	var got []generation.Stage
	for {
		select {
		case p := <-ch:
			got = append(got, p.Stage)
		default:
			return got
		}
	}
}

func TestSubscribeShot_AMessagePublishedWhileTheStateIsReadIsNotLost(t *testing.T) {
	s := &GenerationService{progress: newBroker[generation.Progress]()}
	// The Shot reads as GENERATING, but its work ends before the read returns.
	read := shotReader(domain.ShotGenerating, func() {
		s.progress.Publish("s1", generation.Progress{ShotID: "s1", Stage: generation.StageDone})
	})

	ch, cancel, err := s.subscribeShot(context.Background(), "s1", read)
	require.NoError(t, err)
	defer cancel()

	assert.Equal(t, []generation.Stage{generation.StageDone}, drain(ch), "done reaches the client; the stale GENERATING snapshot is not queued behind it")
}

func TestSubscribeShot_AShotThatStoppedWhileTheStateIsReadEndsWithIdle(t *testing.T) {
	s := &GenerationService{progress: newBroker[generation.Progress]()}
	read := shotReader(domain.ShotEditing, func() {
		s.progress.Publish("s1", generation.Progress{ShotID: "s1", Stage: generation.StageGenerating})
	})

	ch, cancel, err := s.subscribeShot(context.Background(), "s1", read)
	require.NoError(t, err)
	defer cancel()

	assert.Equal(t, []generation.Stage{generation.StageGenerating, generation.StageIdle}, drain(ch), "idle is the last word")
}

func TestSubscribeShot_TheFirstMessageIsTheCurrentStage(t *testing.T) {
	for state, want := range map[domain.ShotState]generation.Progress{
		domain.ShotGenerating: {ShotID: "s1", Stage: generation.StageGenerating},
		domain.ShotJudging:    {ShotID: "s1", Action: generation.ActionJudge, Stage: generation.StageJudging},
		domain.ShotRepairing:  {ShotID: "s1", Action: generation.ActionRepair, Stage: generation.StageRepairing},
		domain.ShotPlanning:   {ShotID: "s1", Stage: generation.StageIdle},
		domain.ShotEditing:    {ShotID: "s1", Stage: generation.StageIdle},
	} {
		s := &GenerationService{progress: newBroker[generation.Progress]()}
		ch, cancel, err := s.subscribeShot(context.Background(), "s1", shotReader(state, nil))
		require.NoError(t, err)
		select {
		case got := <-ch:
			assert.Equal(t, want, got, state)
		case <-time.After(time.Second):
			t.Errorf("%s: no first message", state)
		}
		cancel()
	}
}
