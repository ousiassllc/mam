package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	keyring.MockInit()
	cfg, err := config.Load([]string{"-data-dir", t.TempDir(), "-port", "0", "-no-browser"}, func(string) string { return "" }, os.Stderr)
	require.NoError(t, err)
	return cfg
}

func TestBuildServices_ACleanupClosesEverythingItOpened(t *testing.T) {
	cfg := testConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	services, cleanup, err := buildServices(ctx, cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	require.NotNil(t, services)
	assert.Equal(t, "ok", services.Health.Check(ctx).Database)

	cleanup() // queue, services, Judge, then the database
	assert.Equal(t, "unavailable", services.Health.Check(ctx).Database, "the database is closed")
}

func TestBuildServices_AFailureLateInTheBuildReleasesTheDatabase(t *testing.T) {
	cfg := testConfig(t)
	bad := filepath.Join(t.TempDir(), "model.json")
	require.NoError(t, os.WriteFile(bad, []byte(`{"nope": 1}`), 0o600))
	cfg.ModelConfigPath = bad // fails after the database is open

	services, cleanup, err := buildServices(context.Background(), cfg, slog.New(slog.DiscardHandler))
	require.Error(t, err)
	assert.Nil(t, services)
	assert.Nil(t, cleanup)

	// The database file lock is free again: opening the same data dir succeeds.
	cfg.ModelConfigPath = ""
	_, cleanup, err = buildServices(context.Background(), cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	cleanup()
}
