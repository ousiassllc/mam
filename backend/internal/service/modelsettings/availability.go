package modelsettings

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// SetAvailability registers whether a model can be chosen now (gentest.Service.Availability: no API key, not signed in, the vendor's breaker, a missing custom
// node), so that the choices of the assignment say so (#1013): ImageOption.Availability and VideoOption.Availability, and View.Unusable for the routes that
// still name such a model. The second result is false for a model it does not know (then the model counts as usable). It is asked on every View, so a
// key saved a moment ago applies at once. It must be set before the service is used; nil (the default) knows none.
func (s *Service) SetAvailability(f func(modelID string) (modelfilter.Availability, bool)) {
	s.availability = f
}

// availabilityOf is the availability of the model: usable when none is registered or the model is not known to it.
func (s *Service) availabilityOf(m domain.ModelName) modelfilter.Availability {
	if s.availability != nil {
		if a, ok := s.availability(string(m)); ok {
			return a
		}
	}
	return modelfilter.Usable()
}

// UnusableRoute is a saved route that names a model that cannot be used now: the assignment screen warns about it (#1013).
type UnusableRoute struct {
	// Media is "image" or "video"; Key is the image purpose or the Quality Policy; Role is "primary" or "fallback".
	Media, Key, Role string
	Model            domain.ModelName
	Availability     modelfilter.Availability
}

// unusableRoutes lists the routes of cfg that name a model that cannot be used now, images first, in the order of the screen.
func (s *Service) unusableRoutes(cfg modeladapter.Config) []UnusableRoute {
	var out []UnusableRoute
	add := func(media, key, role string, m domain.ModelName) {
		if a := s.availabilityOf(m); !a.Available {
			out = append(out, UnusableRoute{Media: media, Key: key, Role: role, Model: m, Availability: a})
		}
	}
	for _, p := range modeladapter.ImagePurposes() {
		add("image", string(p), "primary", cfg.Images[p].Primary.Model)
		add("image", string(p), "fallback", cfg.Images[p].Fallback.Model)
	}
	for _, q := range domain.QualityPolicies() {
		add("video", string(q), "primary", cfg.Policies[q].Primary.Model)
		add("video", string(q), "fallback", cfg.Policies[q].Fallback.Model)
	}
	return out
}

// withAvailability puts the availability into the options of View.
func (s *Service) withAvailability(images []ImageOption, videos []VideoOption) {
	for i := range images {
		images[i].Availability = s.availabilityOf(images[i].Model)
	}
	for i := range videos {
		videos[i].Availability = s.availabilityOf(videos[i].Model)
	}
}

// Item is the choice as the filter reads it (modelfilter.Condition.Match).
func (o ImageOption) Item() modelfilter.Item {
	return choiceItem(o.Model, modelfilter.KindImage, o.Availability)
}

// Item is the choice as the filter reads it (modelfilter.Condition.Match).
func (o VideoOption) Item() modelfilter.Item {
	return choiceItem(o.Model, modelfilter.KindVideo, o.Availability)
}

func choiceItem(m domain.ModelName, kind modelfilter.Kind, a modelfilter.Availability) modelfilter.Item {
	origin, provider := modelfilter.OriginAPI, string(modeladapter.VendorOf(m))
	if _, _, _, local := localcfg.ParseID(string(m)); local {
		origin, provider = modelfilter.OriginLocal, "local"
	}
	return a.Item(modelfilter.Item{
		ID: string(m), Name: string(m), Kinds: []modelfilter.Kind{kind}, Commercial: modellicense.Of(string(m)).Commercial, Installed: true, Origin: origin, Provider: provider,
	})
}

// ImageModelsWhere lists the image choices that pass the condition, in their order.
func (v View) ImageModelsWhere(c modelfilter.Condition) []ImageOption {
	return modelfilter.Filter(c, v.ImageModels, ImageOption.Item)
}

// VideoModelsWhere lists the video choices that pass the condition, in their order.
func (v View) VideoModelsWhere(c modelfilter.Condition) []VideoOption {
	return modelfilter.Filter(c, v.VideoModels, VideoOption.Item)
}
