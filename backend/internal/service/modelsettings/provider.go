package modelsettings

import (
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// The API provider the cloud video models of viaprovider.Models are called through (spec/architecture/model-adapters.md §9.6) is the
// key video_provider of the same file; it is saved and applied at once like the assignment (SetOnChange).
const keyVideoProvider = "video_provider"

// Provider is an API provider (viaprovider.Provider): "official", "openrouter", "fal" or "replicate".
type Provider = viaprovider.Provider

// ProviderOfficial is the default provider (viaprovider's): the handler may not import the agent packages.
const ProviderOfficial = viaprovider.Official

// Providers lists the providers in the order the screen shows them.
func Providers() []Provider { return viaprovider.Providers() }

// OwnVendors are the credential keys of the vendors of viaprovider.Models, the models an API aggregator stands in for: their own APIs' keys, which are not used while an
// aggregator is chosen. The handler may not import the agent packages, so it asks here.
func OwnVendors() []string {
	models := viaprovider.Models()
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, string(modeladapter.VendorOf(m)))
	}
	return out
}

// ProviderModel is how the selected provider serves one video model: whether it does, and when it does not, why (a sentence that says
// where the model can be used).
type ProviderModel struct {
	Model     domain.ModelName
	Label     string
	Supported bool
	Why       string
	// PerSecond are the provider's prices of a second of video by resolution (resolutions ascending), empty for a model it does not serve.
	PerSecond []ProviderPrice
}

// ProviderPrice is the price of a second of video at a resolution (USD).
type ProviderPrice struct {
	Resolution string
	USD        float64
}

// ProviderView is the saved provider and how it serves the models.
type ProviderView struct {
	// Pinned: the file was fixed by --model-config; the provider is shown but cannot be changed.
	Pinned   bool
	Selected Provider
	Models   []ProviderModel
}

// VideoProvider is the saved provider and how it serves each model it concerns: the file's, else the official one (a saved change is
// in effect already: it is applied when it is saved). It fails when the file cannot be read as a Model Adapter setting.
func (s *Service) VideoProvider() (ProviderView, error) {
	defer settingsfile.Lock(s.path)()
	cfg, err := s.load()
	if err != nil {
		return ProviderView{}, err
	}
	v := ProviderView{Pinned: s.pinned, Selected: cfg.VideoProvider.Selected()}
	for _, m := range viaprovider.Models() {
		pm := ProviderModel{Model: m, Label: viaprovider.ModelLabel(m), Supported: true}
		if r, ok := viaprovider.RouteOf(v.Selected, m); ok {
			pm.Supported, pm.Why = r.Supported(), r.Why
		}
		if caps, ok := cfg.VideoCaps(m); ok && pm.Supported {
			for _, res := range caps.Resolutions {
				pm.PerSecond = append(pm.PerSecond, ProviderPrice{Resolution: res, USD: cfg.VideoPerSecond(modeladapter.VideoTarget{Model: m, Resolution: res})})
			}
		}
		v.Models = append(v.Models, pm)
	}
	return v, nil
}

// SaveVideoProvider stores the provider; the official one removes the key (the default) and a file left with no key is removed. A provider
// that does not exist is *InvalidError, a pinned file is refused (ErrPinned); nothing is written then.
func (s *Service) SaveVideoProvider(p Provider) error {
	if !p.Valid() {
		return &InvalidError{Message: "API プロバイダーは公式・OpenRouter・fal.ai・Replicate のどれかを選んでください"}
	}
	if s.pinned {
		return ErrPinned
	}
	defer settingsfile.Lock(s.path)()
	raw, err := s.readKeys()
	if err != nil {
		return err
	}
	if p == viaprovider.Official {
		delete(raw, keyVideoProvider)
	} else {
		data, _ := json.Marshal(p)
		raw[keyVideoProvider] = data
	}
	return s.committed(raw)
}
