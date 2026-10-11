package repository

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
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

// holdWriter keeps the single writer busy until the returned func is called; that returns the Write's result.
func holdWriter(db *DB) func() error {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- db.Write(context.Background(), func(context.Context, *sql.Tx) error { close(entered); <-release; return nil })
	}()
	<-entered
	return func() error { close(release); return <-done }
}

func TestWrite_PanicIsLoggedWithItsStack(t *testing.T) {
	db, logs := openInternal(t), &bytes.Buffer{}
	db.logger = slog.New(slog.NewTextHandler(logs, nil))
	_ = db.Write(context.Background(), func(context.Context, *sql.Tx) error { panic("kaboom") })
	assert.Contains(t, logs.String(), "stack=", "the log must say where the panic came from")
}

func TestPing_ChecksTheWriterPoolToo(t *testing.T) {
	db := openInternal(t)
	require.NoError(t, db.Ping(context.Background()))

	require.NoError(t, db.write.Close())
	assert.Error(t, db.Ping(context.Background()), "a healthy reader does not make a broken writer healthy")
}

func TestPing_FailsAtTheDeadlineWhileTheWriterIsStuck(t *testing.T) {
	db := openInternal(t)
	release := holdWriter(db)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, db.Ping(ctx), context.DeadlineExceeded)

	require.NoError(t, release())
	assert.NoError(t, db.Ping(context.Background()), "healthy again once the writer is free")
}

func TestPing_GivesUpAfterTheOwnTimeoutWhenTheCallerSetsNoDeadline(t *testing.T) {
	old := pingTimeout
	pingTimeout = 50 * time.Millisecond
	t.Cleanup(func() { pingTimeout = old })
	db := openInternal(t)
	release := holdWriter(db)

	start := time.Now()
	assert.ErrorIs(t, db.Ping(context.Background()), context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second)

	require.NoError(t, release())
}
