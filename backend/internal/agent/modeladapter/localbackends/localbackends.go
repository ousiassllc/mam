// Package localbackends builds the backends of the whole setting: the built-in vendors (modeladapter.NewBackends)
// and a backend for every model defined on a registered endpoint (spec/architecture/local-models.md §2). The
// settings service calls it again whenever the user saves an endpoint or a model, and the result goes to
// modeladapter.Adapter.Reload.
package localbackends

import (
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/openaicompat"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

// Options of Build.
type Options struct {
	// PollEvery is the wait between two status requests of a running ComfyUI job (0: 2 seconds).
	PollEvery time.Duration
	// StagingDir is where downloaded videos are staged until the caller stores them.
	StagingDir string
	// Vendor are the options of the built-in vendors' backends.
	Vendor []modeladapter.BackendOption
	// Prepare, when set, runs before every call of a model of a registered endpoint (the bundled ComfyUI is
	// started here when it is not running). A failure is the failure of the call.
	Prepare PrepareFunc
}

// Build returns the backends of cfg. A model of an endpoint that cannot be built (a bad base URL) is
// left out, so the model has no backend and the Adapter routes around it; configcheck refuses such a
// setting before it is saved.
func Build(cfg modeladapter.Config, keys credential.Store, o Options) modeladapter.Backends {
	b := modeladapter.NewBackends(cfg, keys, nil, o.PollEvery, append([]modeladapter.BackendOption{modeladapter.WithStagingDir(o.StagingDir)}, o.Vendor...)...)
	clients := map[string]*localhttp.Client{}
	for name, e := range cfg.Endpoints {
		c, err := localhttp.New(name, e, keys, o.StagingDir)
		if err != nil {
			continue
		}
		c.PollEvery = o.PollEvery
		clients[name] = c
	}
	for key, m := range cfg.LocalModels {
		endpoint, name, ok := localcfg.SplitKey(key)
		client := clients[endpoint]
		if !ok || client == nil {
			continue
		}
		id := domain.ModelName(localcfg.ID(cfg.Endpoints[endpoint].Kind, endpoint, name))
		switch cfg.Endpoints[endpoint].Kind {
		case localcfg.KindComfyUI:
			backend := &comfyui.Backend{Client: client, Model: m}
			if m.Media == localcfg.MediaVideo {
				b.Video[id] = wrapVideo(endpoint, o.Prepare, backend)
			} else {
				b.Image[id] = wrapImage(endpoint, o.Prepare, backend)
			}
		case localcfg.KindOpenAICompat:
			if m.Media == localcfg.MediaImage {
				b.Image[id] = wrapImage(endpoint, o.Prepare, openaicompat.Backend{Client: client, CostPerImage: m.CostPerImage})
			}
		}
	}
	return b
}

// Publish makes the models of cfg selectable everywhere a model name is validated (modelreg, read by
// domain.ImageModels and domain.VideoModels): the locks of a Shot, the Director's commands, the settings
// screen. Call it whenever the backends are rebuilt.
func Publish(cfg modeladapter.Config) {
	modeladapter.PublishLocal(cfg.Endpoints, cfg.LocalModels)
	modelreg.Set(localcfg.IDs(cfg.Endpoints, cfg.LocalModels, localcfg.MediaImage), localcfg.IDs(cfg.Endpoints, cfg.LocalModels, localcfg.MediaVideo))
}
