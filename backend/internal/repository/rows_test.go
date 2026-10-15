package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fiftyRows = `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 50) SELECT i FROM n`

func scanInt(row rowScanner) (int, error) {
	var i int
	return i, row.Scan(&i)
}

// writerIsFree fails the test when a query left a connection of the writer pool (which has one)
// checked out: the next write would wait for it until the deadline.
func writerIsFree(t *testing.T, db *DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, db.Write(ctx, func(context.Context, *sql.Tx) error { return nil }), "the writer's only connection is still held")
}

func TestQueryAll_ReturnsTheRowsInOrderAndFreesTheConnection(t *testing.T) {
	db := openInternal(t)

	got, err := queryAll(context.Background(), db.write, scanInt, fiftyRows)
	require.NoError(t, err)
	require.Len(t, got, 50)
	assert.Equal(t, 1, got[0])
	assert.Equal(t, 50, got[49])
	writerIsFree(t, db)

	none, err := queryAll(context.Background(), db.write, scanInt, fiftyRows+` WHERE i > ?`, 50)
	require.NoError(t, err)
	assert.Empty(t, none)
	writerIsFree(t, db)
}

func TestQueryAll_AScanFailureFreesTheConnection(t *testing.T) {
	db := openInternal(t)
	boom := errors.New("boom")
	calls := 0

	_, err := queryAll(context.Background(), db.write, func(row rowScanner) (int, error) {
		calls++
		if calls == 3 {
			return 0, boom
		}
		return scanInt(row)
	}, fiftyRows)

	require.ErrorIs(t, err, boom)
	assert.Equal(t, 3, calls, "the scan stops at the first failure")
	writerIsFree(t, db)
}

func TestQueryAll_AQueryFailureIsReturnedAndFreesTheConnection(t *testing.T) {
	db := openInternal(t)

	_, err := queryAll(context.Background(), db.write, scanInt, `SELECT no_such_column FROM no_such_table`)
	require.Error(t, err)
	writerIsFree(t, db)

	// A row whose columns do not fit the scan target is a scan failure too.
	_, err = queryAll(context.Background(), db.write, func(row rowScanner) (int, error) {
		var a, b int
		return a, row.Scan(&a, &b)
	}, fiftyRows)
	require.Error(t, err)
	writerIsFree(t, db)
}

func TestQueryAll_ACancelledContextFreesTheConnection(t *testing.T) {
	db := openInternal(t)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := queryAll(cancelled, db.write, scanInt, fiftyRows)
	require.ErrorIs(t, err, context.Canceled)
	writerIsFree(t, db)

	// Cancelled while the rows are being read: the iteration ends with the context's error.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = queryAll(ctx, db.write, func(row rowScanner) (int, error) {
		cancel()
		time.Sleep(50 * time.Millisecond) // let database/sql notice and close the rows
		return scanInt(row)
	}, fiftyRows)
	require.ErrorIs(t, err, context.Canceled)
	writerIsFree(t, db)
}
