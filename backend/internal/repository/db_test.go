package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

func openTestDB(t *testing.T) *repository.DB {
	t.Helper()
	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func exec(t *testing.T, db *repository.DB, query string, args ...any) error {
	t.Helper()
	return db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	})
}

func TestOpen_UsesWALAndForeignKeys(t *testing.T) {
	db := openTestDB(t)

	var mode string
	require.NoError(t, db.Reader().QueryRow(`PRAGMA journal_mode`).Scan(&mode))
	assert.Equal(t, "wal", mode)

	var fk int
	require.NoError(t, db.Reader().QueryRow(`PRAGMA foreign_keys`).Scan(&fk))
	assert.Equal(t, 1, fk)
}

func TestOpen_IsIdempotentAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mam.db")
	db, err := repository.Open(context.Background(), path, nil)
	require.NoError(t, err)
	require.NoError(t, exec(t, db, `INSERT INTO job (id, kind) VALUES ('j1', 'generate')`))
	require.NoError(t, db.Close())

	db, err = repository.Open(context.Background(), path, nil)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var n int
	require.NoError(t, db.Reader().QueryRow(`SELECT COUNT(*) FROM job`).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestReader_RejectsWrites(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Reader().Exec(`INSERT INTO job (id, kind) VALUES ('j1', 'generate')`)
	assert.Error(t, err)
}

func TestWrite_RollsBackOnErrorAndPanic(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	boom := errors.New("boom")
	err := db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO job (id, kind) VALUES ('j1', 'generate')`); err != nil {
			return err
		}
		return boom
	})
	assert.ErrorIs(t, err, boom)

	err = db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, `INSERT INTO job (id, kind) VALUES ('j2', 'generate')`)
		panic("kaboom")
	})
	assert.ErrorContains(t, err, "kaboom")

	var n int
	require.NoError(t, db.Reader().QueryRow(`SELECT COUNT(*) FROM job`).Scan(&n))
	assert.Zero(t, n)

	// The writer must survive a panic.
	require.NoError(t, exec(t, db, `INSERT INTO job (id, kind) VALUES ('j3', 'generate')`))
}

func TestWrite_SerializesConcurrentWriters(t *testing.T) {
	db := openTestDB(t)
	var inFlight, maxInFlight atomic.Int32

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			err := db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
				cur := inFlight.Add(1)
				defer inFlight.Add(-1)
				for {
					prev := maxInFlight.Load()
					if cur <= prev || maxInFlight.CompareAndSwap(prev, cur) {
						break
					}
				}
				_, _ = tx.ExecContext(ctx, `INSERT INTO job (id, kind) VALUES (?, 'generate')`, cur)
				return nil
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	assert.Equal(t, int32(1), maxInFlight.Load())
}

func TestWrite_AfterCloseReturnsErrClosed(t *testing.T) {
	db := openTestDB(t)
	require.NoError(t, db.Close())
	err := db.Write(context.Background(), func(context.Context, *sql.Tx) error { return nil })
	assert.ErrorIs(t, err, repository.ErrClosed)
}

func TestWrite_CanceledContextIsNotExecuted(t *testing.T) {
	db := openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := db.Write(ctx, func(context.Context, *sql.Tx) error { called = true; return nil })
	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, called)
}
