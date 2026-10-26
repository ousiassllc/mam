package modelmanager

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
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

// Brief is the part of Status that the tool cards of the home page show.
type Brief struct {
	// Supported: the bundled engine exists on this OS. Installed: it is installed and its files are there.
	Supported, Installed bool
	Process              proc.State
	// External is the number of registered ComfyUI endpoints that are not the bundled one.
	External int
}

// Brief reads what the home page needs: the install record, one stat of the engine's interpreter, the process state and the
// model setting file. Unlike Status (Engine) it writes nothing (no directory next to mam.exe is made, no probe file is
// created), does not ask the disk for its free space or the key store about the endpoints, and does not list the builds.
func (s *Service) Brief(context.Context) (Brief, error) {
	names, err := s.Models.EndpointNames(localcfg.KindComfyUI)
	if err != nil {
		return Brief{}, err
	}
	b := Brief{Supported: s.bundled, Installed: s.engine.Status().Installed, Process: s.proc.Status().State}
	for _, name := range names {
		if name != BundledEndpoint {
			b.External++
		}
	}
	return b, nil
}
