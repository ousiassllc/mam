package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
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

// A Shot left JUDGING whose judge job the queue fails at start-up (its attempts are used up) has no
// handler left to release it: the start releases it itself (#436).
func TestBuildServices_ARestartReleasesAJudgingShotWhoseJudgeJobFailedAtRecovery(t *testing.T) {
	cfg := testConfig(t)
	ctx := context.Background()
	_, cleanup, err := buildServices(ctx, cfg, slog.New(slog.DiscardHandler)) // creates the database
	require.NoError(t, err)
	cleanup()

	db, err := repository.Open(ctx, cfg.DBPath(), slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	const projectID = "p-judging"
	require.NoError(t, repository.NewProjectRepository(db).Create(ctx, repository.NewProject{
		Project: domain.Project{
			ID: projectID, State: domain.StatePlanning, Mode: domain.ModeDirector, ConfirmationLevel: domain.ConfirmKeyCheck,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced,
		},
		Brief: domain.Brief{Subject: "美容液"},
	}))
	plans := repository.NewPlanRepository(db)
	require.NoError(t, plans.SavePlan(ctx, projectID,
		domain.ScenePlan{Scenes: []domain.PlannedScene{{Title: "A", Shots: []domain.PlannedShot{{Objective: "one", DurationSeconds: 3}}}}},
		domain.Storyboard{Shots: [][]domain.ShotStoryboard{{{Description: "x"}}}}))
	scenes, err := plans.ListScenes(ctx, projectID)
	require.NoError(t, err)
	shots, err := plans.ListShots(ctx, scenes[0].ID)
	require.NoError(t, err)
	shotID := shots[0].ID
	gens := repository.NewGenerationRepository(db)
	require.NoError(t, gens.SetShotState(ctx, shotID, domain.ShotJudging))
	jobs := repository.NewJobRepository(db)
	job, err := jobs.Create(ctx, domain.JobJudge, json.RawMessage(`{"take_id":"t","shot_id":"`+shotID+`"}`))
	require.NoError(t, err)
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE job SET status = 'running', attempts = 3 WHERE id = ?`, job.ID)
		return err
	}))
	require.NoError(t, db.Close())

	_, cleanup, err = buildServices(ctx, cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	cleanup()

	db, err = repository.Open(ctx, cfg.DBPath(), slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	got, err := repository.NewGenerationRepository(db).GetShot(ctx, shotID)
	require.NoError(t, err)
	assert.Equal(t, domain.ShotPlanning, got.Shot.State)
}

// After a restart nothing else wakes the editing worker: an AUTO Project that waits in AUDIO for its
// audio is looked at again by the start itself (#349).
func TestBuildServices_ARestartResumesTheEditingFollowUpsLeftUndone(t *testing.T) {
	cfg := testConfig(t)
	ctx := context.Background()
	_, cleanup, err := buildServices(ctx, cfg, slog.New(slog.DiscardHandler)) // creates the database
	require.NoError(t, err)
	cleanup()

	db, err := repository.Open(ctx, cfg.DBPath(), slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	const projectID = "p-restart"
	require.NoError(t, repository.NewProjectRepository(db).Create(ctx, repository.NewProject{
		Project: domain.Project{
			ID: projectID, State: domain.StateAudio, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmFullAuto,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced,
		},
		Brief: domain.Brief{Subject: "美容液"},
	}))
	_, err = repository.NewEditorRepository(db).SaveEditTimeline(ctx, projectID, "", "", domain.EditTimeline{})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	logs := &syncBuffer{}
	_, cleanup, err = buildServices(ctx, cfg, slog.New(slog.NewTextHandler(logs, nil)))
	require.NoError(t, err)
	defer cleanup()

	assert.Eventually(t, func() bool {
		out := logs.String()
		return strings.Contains(out, "AUTO editing stage") && strings.Contains(out, projectID)
	}, 15*time.Second, 10*time.Millisecond, "the AUTO chain tried the audio stage of the waiting Project; logs:\n%s", logs.String())
}
