package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media"
)

func TestKeyedLock_DropsTheEntryOfAKeyNobodyUses(t *testing.T) {
	var k keyedLock
	for _, key := range []string{"a", "b", "a"} {
		unlock, err := k.lock(context.Background(), key)
		require.NoError(t, err)
		unlock()
	}

	assert.Empty(t, k.entries)
}

func TestKeyedLock_WaiterGivesUpWhenItsContextEnds(t *testing.T) {
	var k keyedLock
	unlock, err := k.lock(context.Background(), "a")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan error, 1)
	go func() {
		_, err := k.lock(ctx, "a")
		got <- err
	}()
	cancel()

	select {
	case err := <-got:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter was not released by the cancellation")
	}
	unlock()
	assert.Empty(t, k.entries, "neither the holder nor the cancelled waiter leaves an entry")
}

func TestKeyedLock_SecondCallerGetsTheLockAfterTheFirstUnlocks(t *testing.T) {
	var k keyedLock
	unlock, err := k.lock(context.Background(), "a")
	require.NoError(t, err)
	got := make(chan func(), 1)
	go func() {
		u, _ := k.lock(context.Background(), "a")
		got <- u
	}()
	select {
	case <-got:
		t.Fatal("the lock was given while held")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()

	select {
	case u := <-got:
		u()
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter never got the lock")
	}
	assert.Empty(t, k.entries)
}

func TestTakeService_UsePartialStopsWaitingForTheShotWhenCancelled(t *testing.T) {
	s := &TakeService{tools: &media.Tools{}}
	unlock, err := s.compose.lock(context.Background(), "shot-1") // a composition of the Shot is running
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
