package modelmanager

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// EndpointRef is a ComfyUI endpoint the user can use.
type EndpointRef struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Bundled: the managed endpoint of the bundled engine (its address follows the engine's port).
	Bundled bool `json:"bundled"`
}

// Status is the state of the model manager for the settings screen: the engine, the ComfyUI endpoints, and
// whether a trial generation is possible.
type Status struct {
	Engine EngineView `json:"engine"`
	// Endpoints are the registered ComfyUI endpoints, the bundled one first. An external one lists the installed
	// models only (ExternalInventory); mam installs nothing there.
	Endpoints []EndpointRef `json:"endpoints"`
	// Pinned: the model setting file was fixed by --model-config: the managed endpoint cannot be written, so models
	// cannot be installed.
	Pinned bool `json:"pinned"`
}

// Status describes the engine and the ComfyUI endpoints.
func (s *Service) Status(context.Context) (Status, error) {
	view, err := s.Models.View()
	if err != nil {
		return Status{}, err
	}
	st := Status{Engine: s.Engine(), Pinned: view.Pinned, Endpoints: []EndpointRef{}}
	for _, ep := range view.Endpoints {
		if ep.Kind == localcfg.KindComfyUI {
			ref := EndpointRef{Name: ep.Name, URL: ep.BaseURL, Bundled: ep.Name == BundledEndpoint}
			if ref.Bundled {
				st.Endpoints = append([]EndpointRef{ref}, st.Endpoints...)
			} else {
				st.Endpoints = append(st.Endpoints, ref)
			}
		}
	}
	return st, nil
}
