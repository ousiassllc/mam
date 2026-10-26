// Package workflows is the ComfyUI workflows saved in mam: the list, the editing (rename, input assignment, a new
// graph), the import (a pasted API-format JSON, or from a ComfyUI server's own saved workflows and history), the
// check against the connected server, the run of one workflow with given inputs, and the address of the node
// screen that is embedded (spec/architecture/comfy/comfy-workflows.md). The screens and the MCP tools call it.
//
// A workflow is one global row (usable by every Project) with a version that goes up whenever its graph or its
// input assignment changes after it could run; every version is kept, so a Take that names comfy:<id>@v<N> can be
// traced and an older version can still run. A workflow that can run is published to the Model Adapter as a
// model of the ComfyUI endpoint it names (localcfg.PublishWorkflows, modelreg), so that it is a choice for image
// purposes and Quality Policies and pinnable as the Video Model of a Shot.
//
// Like localmodels it does not import package service: its refusals are ErrNotFound, ErrExists and *InvalidError.
package workflows

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// Errors of the service.
var (
	// ErrNotFound: no such workflow, or no such version of it.
	ErrNotFound = errors.New("workflows: not found")
	// ErrExists: a workflow with that name exists already.
	ErrExists = errors.New("workflows: already exists")
)

// InvalidError is a refused input; Message is shown to the user.
type InvalidError = modelsettings.InvalidError

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

// Engine is the bundled ComfyUI (modelmanager.Service): the node screen and a run start it when it is stopped.
type Engine interface {
	StartEngine(ctx context.Context) (proc.Status, error)
	Engine() modelmanager.EngineView
}

// AssetLoader reads an image a Project has (its asset), for the inputs of a run: the bytes and the MIME type.
type AssetLoader func(ctx context.Context, projectID, assetID string) (data []byte, mimeType string, err error)

// Deps are what the Service needs.
type Deps struct {
	Repo *comfywf.Repository
	// Models reloads the Model Adapter when a workflow changes (the endpoints are read from the Adapter's setting).
	Models  *localmodels.Service
	Adapter *modeladapter.Adapter
	Keys    credential.Store
	// Engine is the bundled ComfyUI; nil when the app has none (an endpoint named bundled-comfyui is then not started).
	Engine Engine
	// StagingDir is where a downloaded result is staged until it is stored.
	StagingDir string
	// RunsDir holds the results of run_workflow (<RunsDir>/<run id>/).
	RunsDir   string
	LoadAsset AssetLoader
	// Probe sends the requests to a ComfyUI server; nil selects the client for registered endpoints.
	Probe  httpclient.Doer
	Logger *slog.Logger
	Now    func() time.Time
}

// Service manages the saved workflows.
type Service struct {
	Deps
	mu     sync.Mutex // serializes the writes (and the publication that follows each)
	bootID string     // tells a run record of this process from one an earlier process left running
	runs   runTracker
}

// New returns the Service. Call Load (or Publish before the settings are read) so the saved workflows are known.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{Deps: d, bootID: domain.NewID()}
}

// BundledEndpoint is the endpoint of the bundled ComfyUI: where a workflow runs unless it names another.
const BundledEndpoint = modelmanager.BundledEndpoint
