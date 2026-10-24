package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// newWorkflows is the service of the ComfyUI workflows saved in mam. Its workflows were published before the
// model settings were read (workflows.Publish); it publishes again whenever one changes.
func newWorkflows(cfg config.Config, db *repository.DB, lm *localmodels.Service, adapter *modeladapter.Adapter, mgr *modelmanager.Service,
	projects *service.ProjectService, keys credential.Store, backends localbackends.Options, logger *slog.Logger,
) *workflows.Service {
	return workflows.New(workflows.Deps{
		Repo: comfywf.New(db), Models: lm, Adapter: adapter, Keys: keys, Engine: mgr,
		StagingDir: backends.StagingDir, RunsDir: filepath.Join(cfg.DataDir, "workflow-runs"), Logger: logger,
		LoadAsset: func(ctx context.Context, projectID, assetID string) ([]byte, string, error) {
			f, err := projects.Asset(ctx, projectID, assetID)
			if err != nil {
				return nil, "", err
			}
			data, err := os.ReadFile(f.Path)
			return data, f.Asset.MimeType, err
		},
	})
}
