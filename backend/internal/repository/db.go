// Package repository is the SQLite access layer. It depends only on domain.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

// ErrClosed is returned by DB.Write after the DB has been closed.
var ErrClosed = errors.New("repository: db closed")

// baseParams are applied to every connection. WAL keeps readers non-blocking
// while the single writer commits (non-functional.md §3).
const baseParams = "_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"

type writeRequest struct {
	ctx  context.Context
	fn   func(ctx context.Context, tx *sql.Tx) error
	resp chan error
}

// DB wraps a SQLite database opened in WAL mode.
//
// Reads go through Reader (a query-only pool). Every write goes through Write,
// which serializes transactions on one goroutine and one connection, so the
// Orchestrator and background workers are a single logical writer.
type DB struct {
	read   *sql.DB
	write  *sql.DB
	writes chan writeRequest

	logger    *slog.Logger
	closing   chan struct{}
	closeOnce sync.Once
	loopDone  chan struct{}
}

// Open opens (creating if needed) the database at path, applies pending
// migrations and starts the writer goroutine.
func Open(ctx context.Context, path string, logger *slog.Logger) (*DB, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	dsn := dsnFor(path)

	write, err := sql.Open("sqlite", dsn+"&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}
	write.SetMaxOpenConns(1)

	read, err := sql.Open("sqlite", dsn+"&_pragma=query_only(1)")
	if err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}

	d := &DB{
		read:     read,
		write:    write,
		writes:   make(chan writeRequest),
		logger:   logger,
		closing:  make(chan struct{}),
		loopDone: make(chan struct{}),
	}
	if err := d.migrate(ctx); err != nil {
		_ = read.Close()
		_ = write.Close()
		return nil, err
	}
	go d.writeLoop()
	return d, nil
}

func dsnFor(path string) string {
	escaped := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(filepath.ToSlash(path))
	return "file:" + escaped + "?" + baseParams
}

// Reader returns the read-only connection pool. Writes through it fail (query_only).
func (d *DB) Reader() *sql.DB { return d.read }

// Ping verifies both pools can reach the database.
func (d *DB) Ping(ctx context.Context) error {
	if err := d.read.PingContext(ctx); err != nil {
		return fmt.Errorf("ping read db: %w", err)
	}
	return nil
}

// Write runs fn inside a transaction on the single writer and waits for the result.
// The transaction commits when fn returns nil and rolls back otherwise (including panics).
func (d *DB) Write(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	req := writeRequest{ctx: ctx, fn: fn, resp: make(chan error, 1)}
	select {
	case d.writes <- req:
	case <-d.closing:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
	return <-req.resp
}

func (d *DB) writeLoop() {
	defer close(d.loopDone)
	for {
		select {
		case req := <-d.writes:
			req.resp <- d.runWrite(req)
		case <-d.closing:
			return
		}
	}
}

func (d *DB) runWrite(req writeRequest) (err error) {
	if err := req.ctx.Err(); err != nil {
		return err
	}
	tx, err := d.write.BeginTx(req.ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			err = fmt.Errorf("repository: write panicked: %v", r)
			d.logger.Error("db write panicked", slog.Any("panic", r))
		}
	}()
	if err := req.fn(req.ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// Close stops the writer (in-flight transaction finishes first) and closes both pools.
func (d *DB) Close() error {
	var err error
	d.closeOnce.Do(func() {
		close(d.closing)
		<-d.loopDone
		err = errors.Join(d.read.Close(), d.write.Close())
	})
	return err
}
