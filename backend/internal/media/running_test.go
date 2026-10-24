package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Running counts the processes that run, so that the app updater does not restart under an ffmpeg.
func TestRunning_CountsAProcessUntilItEnds(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	exe := filepath.Join(dir, "slow")
	require.NoError(t, os.WriteFile(exe, []byte("#!/bin/sh\ntouch \""+started+"\"\nexec sleep 60\n"), 0o700)) // #nosec G306 -- a test executable
	require.Zero(t, Running())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, _, err := run(ctx, time.Minute, exe); done <- err }()

	require.Eventually(t, func() bool { _, err := os.Stat(started); return err == nil }, 10*time.Second, 5*time.Millisecond)
	assert.Equal(t, 1, Running())

	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("the cancelled process did not end")
	}
	assert.Zero(t, Running())
}
