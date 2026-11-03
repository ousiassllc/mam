package app

import (
	"fmt"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// newModelManager builds the model manager and registers its job handlers on jobs (before the queue starts). The engine and the models live
// in the "comfyui" directory next to the executable on Windows (the uninstaller removes them with the install directory; the portable build keeps them
// next to its exe) and in the data directory on macOS (store.ForSystem: the executable is inside MAM.app, which the auto-update replaces). The custom nodes
// of the allowlist are installed below the same directory (data\custom_nodes), by the model nodes service built from the same Nodes (nodewire.New).
func newModelManager(lm *localmodels.Service, adapter *modeladapter.Adapter, jobs *jobqueue.Queue, table *repository.JobRepository,
	keys credential.Store, backends localbackends.Options, dataDir string, logger *slog.Logger,
) (*modelmanager.Service, error) {
	exe, err := executablePath()
	if err != nil {
		return nil, err
	}
	layout := store.ForSystem(exe, dataDir)
	m, err := modelmanager.New(modelmanager.Deps{
		Layout: layout, Jobs: jobs, JobTable: table, Models: lm, Adapter: adapter,
		Keys: keys, Backends: backends, Logger: logger,
		Nodes: nodes.New(layout), // the engine loads the custom nodes of the allowlist that this manager installed, and no other folder
	})
	if err != nil {
		return nil, fmt.Errorf("model manager: %w", err)
	}
	m.Register()
	return m, nil
}
