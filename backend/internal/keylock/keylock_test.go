package keylock_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/keylock"
)

func TestLocker_WaiterGivesUpWhenItsContextEnds(t *testing.T) {
	var k keylock.Locker
	unlock, err := k.Lock(context.Background(), "a")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan error, 1)
	go func() {
		_, err := k.Lock(ctx, "a")
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
}

func TestLocker_SecondCallerGetsTheLockAfterTheFirstUnlocks(t *testing.T) {
	var k keylock.Locker
	unlock, err := k.Lock(context.Background(), "a")
	require.NoError(t, err)
	got := make(chan func(), 1)
	go func() {
		u, _ := k.Lock(context.Background(), "a")
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
}

func TestLocker_TryLockFailsWhileHeldAndSucceedsAfterUnlock(t *testing.T) {
	var k keylock.Locker
	unlock, ok := k.TryLock("a")
	require.True(t, ok)

	_, ok = k.TryLock("a")
	assert.False(t, ok, "the key is held")
	other, ok := k.TryLock("b")
	assert.True(t, ok, "another key is free")
	other()

	unlock()
	unlock, ok = k.TryLock("a")
	require.True(t, ok, "the key was released")
	unlock()
}

func TestLocker_TryLockDoesNotTakeTheLockOfAWaiter(t *testing.T) {
	var k keylock.Locker
	unlock, err := k.Lock(context.Background(), "a")
	require.NoError(t, err)
	_, ok := k.TryLock("a")
	assert.False(t, ok)
	unlock()
}

func TestLocker_BusyWhileAKeyIsHeld(t *testing.T) {
	var k keylock.Locker
	assert.False(t, k.Busy())

	unlock, err := k.Lock(context.Background(), "a")
	require.NoError(t, err)
	assert.True(t, k.Busy())

	unlock()
	assert.False(t, k.Busy(), "an entry lives only while its key is held or waited for")
}
