// Package nodewire builds the custom node manager for the app. It is a package of its own, under app, so that app stays within its size (linterly).
package nodewire

import (
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
)

// New builds the custom node manager (the allowlist of nodes the bundled ComfyUI may load) over the model manager's engine and its
// Nodes. Its jobs (target "node:<id>") are the model_install kind's like the models', so the handler it returns takes them and hands every
// other job on to the model manager's handler; the local runtimes then wrap that (app.newLocalRuntimes). It runs before the queue starts.
func New(jobs *jobqueue.Queue, table *repository.JobRepository, mgr *modelmanager.Service, logger *slog.Logger) (*modelnodes.Service, jobqueue.Handler) {
	svc := modelnodes.New(modelnodes.Deps{Nodes: mgr.Nodes, Engine: mgr, Queue: jobs, JobTable: table, Logger: logger})
	return svc, svc.Handler(mgr.HandleInstall)
}
