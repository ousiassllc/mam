package repository

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// Migration files are named <UTC timestamp>_<name>.sql (e.g. 20260929000001_init_schema.sql).
// Timestamp versions plus out-of-order support let parallel branches add migrations
// without renumbering each other. M2 tables/columns are added by their own migrations.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

func (d *DB) migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, d.write, sub, goose.WithAllowOutofOrder(true))
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		d.logger.Info("migration applied", "version", r.Source.Version, "duration", r.Duration)
	}
	return nil
}
