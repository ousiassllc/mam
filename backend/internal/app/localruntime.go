package app

import (
	"fmt"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// newLocalRuntimes builds the manager of the local AI runtimes (GPT-SoVITS) and takes over the model_install handler on jobs
// (before the queue starts, after newModelManager registered its own): the jobs of a runtime are its own, the others still go to
// the model manager. The runtimes live in the "localai" directory next to the executable, like the engine in "comfyui", so that
// the uninstaller removes them with the install directory.
func newLocalRuntimes(jobs *jobqueue.Queue, table *repository.JobRepository, mgr *modelmanager.Service, logger *slog.Logger) (*localruntime.Service, error) {
	exe, err := executablePath()
	if err != nil {
		return nil, err
	}
	rts, err := localruntime.New(localruntime.Deps{Root: store.LocalAIRoot(exe), Queue: jobs, JobTable: table, Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("local runtimes: %w", err)
	}
	rts.Register(mgr.HandleInstall)
	return rts, nil
}
