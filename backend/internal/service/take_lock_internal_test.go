package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media"
)

func TestTakeService_UsePartialStopsWaitingForTheShotWhenCancelled(t *testing.T) {
	s := &TakeService{tools: &media.Tools{}}
	unlock, err := s.compose.Lock(context.Background(), "shot-1") // a composition of the Shot is running
	require.NoError(t, err)
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan error, 1)
	go func() {
		_, err := s.UsePartial(ctx, "shot-1", PartialInput{})
		got <- err
	}()
	cancel()

	select {
	case err := <-got:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("UsePartial kept waiting for the running composition after the request was cancelled")
	}
}
