package mmview

import (
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// TrialModel is a model a trial generation can use: what the form of the trial offers for it.
type TrialModel struct {
	// ID is the #831 identifier ("comfyui:bundled-comfyui/sdxl-base"), the ModelID of GentestInput.
	ID       string         `json:"id"`
	Endpoint string         `json:"endpoint"`
	Name     string         `json:"name"`
	Media    localcfg.Media `json:"media"`
	// Bundled: the model is on the endpoint of the engine mam installed.
	Bundled bool `json:"bundled"`
	// AcceptsReference: the model takes a reference image (an image model) or a first frame (a video model).
	AcceptsReference bool `json:"accepts_reference"`
	// Durations and Resolutions are what a video model offers (the first of each is the default).
	Durations   []float64 `json:"durations,omitempty"`
	Resolutions []string  `json:"resolutions,omitempty"`
}

// TrialModels lists the models of the running setting (the models of the bundled engine, of an external ComfyUI and
// of an OpenAI-compatible server) in key order: the choices of a trial generation.
func TrialModels(s *modelmanager.Service) []TrialModel {
	cfg := s.Adapter.Config()
	out := []TrialModel{}
	for _, key := range slices.Sorted(maps.Keys(cfg.LocalModels)) {
		endpoint, name, ok := localcfg.SplitKey(key)
		ep, registered := cfg.Endpoints[endpoint]
		if !ok || !registered {
			continue
		}
		m := cfg.LocalModels[key]
		if s.HidesModel(localcfg.ID(ep.Kind, endpoint, name)) {
			continue
		}
		tm := TrialModel{
			ID: localcfg.ID(ep.Kind, endpoint, name), Endpoint: endpoint, Name: name, Media: m.Media, Bundled: endpoint == modelmanager.BundledEndpoint,
			AcceptsReference: m.ImageInputs(ep.Kind),
		}
		if caps, ok := m.VideoCaps(); ok {
			tm.AcceptsReference = caps.FirstFrame || caps.MaxReferenceImages > 0
			tm.Durations, tm.Resolutions = caps.Durations, caps.Resolutions
		}
		out = append(out, tm)
	}
	return out
}
