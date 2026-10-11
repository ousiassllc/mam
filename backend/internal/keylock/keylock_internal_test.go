package keylock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocker_DropsTheEntryOfAKeyNobodyUses(t *testing.T) {
	var k Locker
	for _, key := range []string{"a", "b", "a"} {
		unlock, err := k.Lock(context.Background(), key)
		require.NoError(t, err)
		unlock()
	}
	ctx, cancel := context.WithCancel(context.Background())
	unlock, err := k.Lock(context.Background(), "c")
	require.NoError(t, err)
	cancel()
	_, err = k.Lock(ctx, "c") // gives up waiting
	require.ErrorIs(t, err, context.Canceled)
	unlock()

	assert.Empty(t, k.entries)
}
