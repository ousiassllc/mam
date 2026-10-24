package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/bundlezip"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// buildServices opens the database, builds every dependency of the services, recovers the work an
// abnormal termination left behind and starts the job queue. cleanup stops and closes all of it in
// the reverse order of construction (queue, services, Judge, database). It is nil on error, where
// buildServices has already released everything it had built. db is the database: the app updater reads
// the job table through it to see whether any work is in flight, and the MCP server keeps its tokens in it.
func buildServices(ctx context.Context, cfg config.Config, logger *slog.Logger) (_ *service.Services, _ *repository.DB, _ func(), err error) {
	var closers []func()
	release := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	defer func() {
		if err != nil {
			release()
		}
	}()

	// Leftovers of interrupted imports (tens of GB at most) go first, so the sweep below does not walk them.
	// Both sweeps are best effort: the leftovers are harmless garbage, so a failure is only logged.
	staleImports, err := bundlezip.RemoveStaleImports(cfg.AssetsDir())
	if err != nil {
		logger.Warn("clean stale import directories failed", "err", err)
	}
	if len(staleImports) > 0 {
		logger.Warn("removed stale import directories", "count", len(staleImports))
	}

	// Leftovers of interrupted atomic writes and ffmpeg conversions (".<name>.ffmpeg.tmp" in .previews
	// too) must not be mistaken for finished assets.
	removed, err := fsutil.RemoveStaleTemp(cfg.AssetsDir())
	if err != nil {
		logger.Warn("clean stale temp files failed", "err", err)
	}
	if len(removed) > 0 {
		logger.Warn("removed stale temp files", "count", len(removed))
	}

	db, err := repository.Open(ctx, cfg.DBPath(), logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open database: %w", err)
	}
	closers = append(closers, func() {
		if err := db.Close(); err != nil {
			logger.Error("close database", "err", err)
		}
	})

	llmFile, err := settingsFileToLoad(cfg.LLMSettingsFile())
	if err != nil {
		return nil, nil, nil, err
	}
	llmCfg, err := llm.LoadConfig(llmFile)
	if err != nil {
		return nil, nil, nil, err
	}
	keys := credential.NewKeyring()
	reasoning := llm.NewClient(llmCfg, llm.NewBackends(llmCfg, keys, nil), llm.WithLogger(logger))

	modelFile, err := settingsFileToLoad(cfg.ModelSettingsFile())
	if err != nil {
		return nil, nil, nil, err
	}
	modelCfg, err := configcheck.Load(modelFile)
	if err != nil {
		return nil, nil, nil, err
	}
	models := modeladapter.New(modelCfg, modeladapter.NewBackends(modelCfg, keys, nil, 0, modeladapter.WithStagingDir(filepath.Join(cfg.AssetsDir(), ".staging"))), modeladapter.WithLogger(logger))

	judging, err := newJudgeSetup(cfg, logger)
	if err != nil {
		return nil, nil, nil, err
	}
	closers = append(closers, judging.close)

	var previews *media.Tools // ffmpeg for the Take previews and Partial Take Use; nil when missing
	if tools, err := media.Look(); err != nil {
		logger.Warn("Take previews are limited: ffmpeg / ffprobe not found (4K Takes play as they are, Partial Take Use is off)")
	} else {
		previews = &tools
	}

	jevCfg, err := jev.LoadConfig(cfg.JevConfigPath)
	if err != nil {
		return nil, nil, nil, err
	}
	// The Jev shadow run of the Repair Agent works once a TypeSafe API key is stored (credential provider "typesafe").
	shadow, err := jev.New(jevCfg, keys, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	jobTable := repository.NewJobRepository(db)
	jobs := jobqueue.New(jobTable, jobqueue.Options{Logger: logger})
	// Job handlers are registered by service.New (the plan job) and by the features that add more kinds.
	// The services are built before the queue starts: they subscribe to job changes.
	services := service.New(service.Deps{
		DB:          db,
		Jobs:        jobs,
		Credentials: keys,
		LLM:         reasoning,
		Models:      models,
		Logger:      logger,
		AssetsDir:   cfg.AssetsDir(),
		Frames:      judging.frames,
		Faces:       judging.engine(),
		Judge:       judging.config,
		Jev:         shadow,
		Media:       previews,
		Audio:       audioProviders(keys),
	})
	closers = append(closers, services.Close) // runs after jobs.Stop: no job change wakes the workers any more
	if err := recoverWork(ctx, services, jobs, logger); err != nil {
		return nil, nil, nil, err
	}
	jobs.Start(ctx)
	closers = append(closers, jobs.Stop)
	return services, db, release, nil
}

// recoverWork cleans up after an abnormal termination: Shots left GENERATING / REPAIRING are released
// before their jobs are failed, the Shots whose job the queue then failed (a judge job out of attempts,
// a job without handler) are released after it, and the editing follow-ups that the stop left undone are
// woken.
func recoverWork(ctx context.Context, services *service.Services, jobs *jobqueue.Queue, logger *slog.Logger) error {
	released, err := services.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover shots: %w", err)
	}
	if released > 0 {
		logger.Warn("released shots interrupted while generating", "count", released)
	}
	resumed, failed, err := jobs.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover jobs: %w", err)
	}
	// A job the queue failed will never run its handler, which is what releases its Shot.
	released, err = services.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover shots of failed jobs: %w", err)
	}
	if released > 0 {
		logger.Warn("released shots whose job failed at recovery", "count", released)
	}
	if resumed+failed > 0 {
		logger.Warn("recovered unfinished jobs after abnormal termination", "resumed", resumed, "failed", failed)
	}
	// A Reshoot round that was ready, or an AUTO Project that waits for its editing stage, when the
	// process stopped has no job change or Take selection left to wake the worker.
	services.Editor.ResumeEditing()
	return nil
}

// audioProviders are the vendors of the Audio Director: ElevenLabs (TTS, cloned voices, Music,
// Sound Effects; API key "elevenlabs") and Sync Labs (Lip Sync; API key "synclabs").
func audioProviders(keys credential.Store) audiodirector.Providers {
	p := audiodirector.ElevenLabsProviders(audiodirector.NewElevenLabs(audiodirector.Config{}, keys, nil))
	p.LipSync = audiodirector.NewSyncLabs(audiodirector.SyncLabsConfig{}, keys, nil)
	return p
}

// settingsFileToLoad is the path to hand to llm.LoadConfig / configcheck.Load: a pinned file (the
// operator named it) as it is, so a missing one is an error; the default file of the data directory only
// when it exists, where "" selects the defaults. Any other failure to reach it is an error, as it is for a pinned file.
func settingsFileToLoad(path string, pinned bool) (string, error) {
	if pinned {
		return path, nil
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("settings file %s: %w", path, err)
	}
	return path, nil
}
