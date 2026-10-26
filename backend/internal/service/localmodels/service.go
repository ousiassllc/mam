// Package localmodels is the "ローカル・代替モデル" section of the settings screen and of the MCP tools:
// the endpoints the user registered (ComfyUI, OpenAI-compatible servers), the models defined on them,
// the connection test and the API key of each endpoint (spec/architecture/local-models.md, "Service API").
//
// The endpoints and models live in the data directory's models.json, under the keys "endpoints" and
// "local_models", next to the keys modelsettings owns (images, policies); the other keys are kept as they
// are. A saved change takes effect at once: the file is checked as at start-up, the backends are rebuilt
// and swapped into the Model Adapter (calls in flight keep the setting they started with). A file pinned by
// --model-config is read, never written. The assignment of a model to an image purpose or a Quality Policy
// is modelsettings.Service.Save, whose choices include the models defined here.
//
// Like modelsettings it does not import package service: its refusals are ErrPinned, ErrNotFound,
// ErrExists and *InvalidError.
package localmodels

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// Errors of the service.
var (
	// ErrPinned: the operator passed --model-config; the file is never written.
	ErrPinned = modelsettings.ErrPinned
	// ErrNotFound: no such endpoint or model.
	ErrNotFound = errors.New("localmodels: not found")
	// ErrExists: an endpoint or model with that name exists already.
	ErrExists = errors.New("localmodels: already exists")
)

// InvalidError is a refused input; Message is shown to the user (modelsettings.InvalidError).
type InvalidError = modelsettings.InvalidError

func invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

// Deps are what the Service needs.
type Deps struct {
	// Path is the settings file (models.json of the data directory, or the file --model-config named).
	Path string
	// Pinned: Path was named by --model-config.
	Pinned bool
	// Keys holds the API key of each endpoint under localcfg.VendorKey(name).
	Keys credential.Store
	// Adapter is the running Model Adapter; a saved change is swapped into it (Reload).
	Adapter *modeladapter.Adapter
	// Backends are the options the backends are rebuilt with (staging directory, poll interval).
	Backends localbackends.Options
	// Probe sends the requests of the connection test. Nil selects the client for registered endpoints.
	Probe  httpclient.Doer
	Logger *slog.Logger
}

// Service edits the endpoints and local models of one Model Adapter setting file.
type Service struct {
	Deps
}

// New returns the Service.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	return &Service{Deps: d}
}
