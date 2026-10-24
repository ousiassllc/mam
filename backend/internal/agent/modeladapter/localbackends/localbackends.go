// Package localbackends builds the backends of the whole setting: the built-in vendors (modeladapter.NewBackends)
// and a backend for every model defined on a registered endpoint (spec/architecture/local-models.md §2). The
// settings service calls it again whenever the user saves an endpoint or a model, and the result goes to
// modeladapter.Adapter.Reload.
package localbackends

import (
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
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
	// ChatGPTImage is the backend of chatgptid.Model (image generation with a ChatGPT subscription); nil leaves
	// the model without a backend. It is offered for selection (Publish) only while ChatGPTImageEnabled is true.
	ChatGPTImage        modeladapter.ImageBackend
	ChatGPTImageEnabled func() bool
}

// Build returns the backends of cfg. A model of an endpoint that cannot be built (a bad base URL) is
// left out, so the model has no backend and the Adapter routes around it; configcheck refuses such a
// setting before it is saved.
func Build(cfg modeladapter.Config, keys credential.Store, o Options) modeladapter.Backends {
	b := modeladapter.NewBackends(cfg, keys, nil, o.PollEvery, append([]modeladapter.BackendOption{modeladapter.WithStagingDir(o.StagingDir)}, o.Vendor...)...)
	if o.ChatGPTImage != nil {
		b.Image[chatgptid.Model] = o.ChatGPTImage
	}
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
				b.Video[id] = backend
			} else {
				b.Image[id] = backend
			}
		case localcfg.KindOpenAICompat:
			if m.Media == localcfg.MediaImage {
				b.Image[id] = openaicompat.Backend{Client: client, CostPerImage: m.CostPerImage}
			}
		}
	}
	return b
}

// Publish makes the models of cfg selectable everywhere a model name is validated (modelreg, read by
// domain.ImageModels and domain.VideoModels): the locks of a Shot, the Director's commands, the settings
// screen. Call it whenever the backends are rebuilt. The ChatGPT subscription's image model is listed only
// while the user has turned it on, so it is never offered while it would not be called.
func (o Options) Publish(cfg modeladapter.Config) {
	modeladapter.PublishLocal(cfg.Endpoints, cfg.LocalModels)
	images := localcfg.IDs(cfg.Endpoints, cfg.LocalModels, localcfg.MediaImage)
	if o.ChatGPTImage != nil && o.ChatGPTImageEnabled != nil && o.ChatGPTImageEnabled() {
		images = append(images, string(chatgptid.Model))
	}
	modelreg.Set(images, localcfg.IDs(cfg.Endpoints, cfg.LocalModels, localcfg.MediaVideo))
}

// Publish is Options.Publish without the ChatGPT image model.
func Publish(cfg modeladapter.Config) { Options{}.Publish(cfg) }
