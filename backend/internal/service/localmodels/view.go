package localmodels

import (
	"cmp"
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// EndpointView is a registered endpoint as the screen and the tools show it. Concurrency and timeout are the
// values in effect (the defaults applied). It never carries the API key.
type EndpointView struct {
	Name           string
	Kind           localcfg.Kind
	BaseURL        string
	MaxConcurrency int
	TimeoutSeconds int
	Key            KeyStatus
}

// ModelView is a model defined on an endpoint. ID is what take.model_name and cost_ledger_entry.provider hold
// and what image purposes and Quality Policies choose. UsedBy lists where it is chosen (images.<purpose>.primary,
// policies.<policy>.fallback, ...): such a model cannot be deleted.
type ModelView struct {
	Endpoint string
	Name     string
	ID       string
	Kind     localcfg.Kind
	localcfg.Model
	UsedBy []string
}

// View is the saved endpoints and models.
type View struct {
	// Pinned: the file was fixed by --model-config; it is shown but cannot be changed (the API keys can).
	Pinned    bool
	Endpoints []EndpointView
	Models    []ModelView
}

// View reads the saved endpoints and models (the file's; a saved change is applied at once, so it is also
// what runs).
func (s *Service) View() (View, error) {
	cfg, err := s.load()
	if err != nil {
		return View{}, err
	}
	v := View{Pinned: s.Pinned, Endpoints: []EndpointView{}, Models: []ModelView{}}
	for _, name := range slices.Sorted(maps.Keys(cfg.Endpoints)) {
		e := cfg.Endpoints[name]
		key, err := s.keyStatus(name)
		if err != nil {
			s.Logger.Warn("read the API key status of a local endpoint failed", "endpoint", name, "err", err)
			key = KeyStatus{Unavailable: true}
		}
		v.Endpoints = append(v.Endpoints, EndpointView{Name: name, Kind: e.Kind, BaseURL: e.BaseURL, MaxConcurrency: e.Concurrency(), TimeoutSeconds: e.Timeout(), Key: key})
	}
	for _, key := range slices.Sorted(maps.Keys(cfg.LocalModels)) {
		endpoint, name, _ := localcfg.SplitKey(key)
		kind := cfg.Endpoints[endpoint].Kind
		id := localcfg.ID(kind, endpoint, name)
		v.Models = append(v.Models, ModelView{Endpoint: endpoint, Name: name, ID: id, Kind: kind, Model: cfg.LocalModels[key], UsedBy: usedBy(cfg, id)})
	}
	slices.SortStableFunc(v.Models, func(a, b ModelView) int { return cmp.Compare(a.ID, b.ID) })
	return v, nil
}

// EndpointNames lists the names of the saved endpoints of the kind, sorted. Unlike View it does not ask the key store
// about each endpoint: for a read that only counts them (the home page).
func (s *Service) EndpointNames(kind localcfg.Kind) ([]string, error) {
	cfg, err := s.load()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, name := range slices.Sorted(maps.Keys(cfg.Endpoints)) {
		if cfg.Endpoints[name].Kind == kind {
			names = append(names, name)
		}
	}
	return names, nil
}
