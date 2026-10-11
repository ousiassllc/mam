package orchestrator_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

func TestHandle_QueuedJobReturnsWhenItsContextEndsWhileThePreviousOneRuns(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.project(t, seed{brief: completeBrief()})

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	f.llm.onCall = func(agent llm.Agent) error {
		if agent == llm.AgentProducer {
			once.Do(func() { close(entered) })
			<-release
		}
		return nil
	}

	first := make(chan error, 1)
	go func() { first <- f.handle(context.Background(), id) }()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() { second <- f.handle(ctx, id) }()
	cancel()
	select {
	case err := <-second:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the queued job kept waiting for the previous one after its context ended")
	}
	assert.Empty(t, f.chat.texts(), "on shutdown nobody is told")

	close(release)
	require.NoError(t, <-first)
	assert.Equal(t, 1, countAgent(f.llm.called(), llm.AgentProducer))
}
