package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openInternal(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPing_ChecksTheWriterPoolToo(t *testing.T) {
	db := openInternal(t)
	require.NoError(t, db.Ping(context.Background()))

	require.NoError(t, db.write.Close())
	assert.Error(t, db.Ping(context.Background()), "a healthy reader does not make a broken writer healthy")
}

func TestPing_FailsAtTheDeadlineWhileTheWriterIsStuck(t *testing.T) {
	db := openInternal(t)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- db.Write(context.Background(), func(context.Context, *sql.Tx) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, db.Ping(ctx), context.DeadlineExceeded)

	close(release)
	require.NoError(t, <-done)
	assert.NoError(t, db.Ping(context.Background()), "healthy again once the writer is free")
}

func TestPing_GivesUpAfterTheOwnTimeoutWhenTheCallerSetsNoDeadline(t *testing.T) {
	old := pingTimeout
	pingTimeout = 50 * time.Millisecond
	t.Cleanup(func() { pingTimeout = old })
	db := openInternal(t)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- db.Write(context.Background(), func(context.Context, *sql.Tx) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	start := time.Now()
	assert.ErrorIs(t, db.Ping(context.Background()), context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second)

	close(release)
	require.NoError(t, <-done)
}
