package app

import (
	"fmt"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull"
)

// newLocalRuntimes builds the manager of the local AI runtimes (GPT-SoVITS) and takes over the model_install handler on jobs
// (before the queue starts, after newModelManager registered its own): the jobs of a runtime are its own, the others still go to
// the model manager. The runtimes live in the "localai" directory next to the executable, like the engine in "comfyui", so that
// the uninstaller removes them with the install directory.
// The fetch of the models of the Ollama of mam (ollamapull) is built with them: its jobs (target "ollama:<name>") are the next handler of the runtimes' and wrap the model manager's.
func newLocalRuntimes(jobs *jobqueue.Queue, table *repository.JobRepository, next jobqueue.Handler, env func(id string) []string, logger *slog.Logger) (*localruntime.Service, *ollamapull.Service, error) {
	exe, err := executablePath()
	if err != nil {
		return nil, nil, err
	}
	root := store.LocalAIRoot(exe)
	ollama := ollamapull.New(ollamapull.Deps{Root: root, Queue: jobs, JobTable: table, Logger: logger})
	rts, err := localruntime.New(localruntime.Deps{Root: root, Queue: jobs, JobTable: table, Env: env, Logger: logger, HeldBy: ollama.HeldBy})
	if err != nil {
		return nil, nil, fmt.Errorf("local runtimes: %w", err)
	}
	rts.Register(ollama.Handler(next))
	ollama.Runtime = rts
	return rts, ollama, nil
}

// newAudioSettings is the saved choice of the Audio Director's providers (audio.json). It holds no state (the file is read on every call),
// so the services (the run reads it) and the settings screen and MCP tools (they write it) each build one over the same file.
func newAudioSettings(cfg config.Config, rts *localruntime.Service, keys credential.Store) *audiosettings.Service {
	return audiosettings.New(cfg.AudioSettingsFile(), rts, service.NewCredentialService(keys))
}
