package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// newModelManager builds the model manager and registers its job handlers on jobs (before the queue starts). The
// engine and the models live in the "comfyui" directory next to the executable, so that the uninstaller, which
// removes the install directory, removes them too and the portable build keeps them next to its exe.
func newModelManager(lm *localmodels.Service, adapter *modeladapter.Adapter, jobs *jobqueue.Queue, table *repository.JobRepository,
	keys credential.Store, backends localbackends.Options, logger *slog.Logger,
) (*modelmanager.Service, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find the executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	m, err := modelmanager.New(modelmanager.Deps{
		Layout: store.ForExecutable(exe), Jobs: jobs, JobTable: table, Models: lm, Adapter: adapter,
		Keys: keys, Backends: backends, Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("model manager: %w", err)
	}
	m.Register()
	return m, nil
}
