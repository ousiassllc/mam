package modelsettings

import (
	"encoding/json"
	"maps"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// SaveImage stores the route of one image purpose; the other routes stay as saved (the whole file is checked as Save
// does, but only this route is: another route that names a model that is refused now does not stop this one). A pinned
// file is refused (ErrPinned), an invalid route is *InvalidError.
func (s *Service) SaveImage(p ImagePurpose, route ImageRoute) error {
	return s.save(func(cfg modeladapter.Config) (Input, error) {
		in := Input{Images: maps.Clone(cfg.Images), Policies: maps.Clone(cfg.Policies)}
		in.Images[p] = route
		return in, checkImage(cfg, p, route, s.gated)
	})
}

// SavePolicy is SaveImage for the route of one Quality Policy.
func (s *Service) SavePolicy(q domain.QualityPolicy, route PolicyRoute) error {
	return s.save(func(cfg modeladapter.Config) (Input, error) {
		in := Input{Images: maps.Clone(cfg.Images), Policies: maps.Clone(cfg.Policies)}
		in.Policies[q] = route
		return in, checkPolicy(cfg, q, route, s.gated)
	})
}

// save writes the assignment that build makes from the saved configuration (and refuses, with its error, when it is not
// valid): see Save.
func (s *Service) save(build func(cfg modeladapter.Config) (Input, error)) error {
	if s.pinned {
		return ErrPinned
	}
	defer settingsfile.Lock(s.path)()
	cfg, err := s.load()
	if err != nil {
		return err
	}
	in, err := build(cfg)
	if err != nil {
		return err
	}
	raw, err := s.readKeys()
	if err != nil {
		return err
	}
	def := modeladapter.DefaultConfig()
	images := map[modeladapter.ImagePurpose]imageRouteJSON{}
	for _, p := range modeladapter.ImagePurposes() {
		if in.Images[p] != def.Images[p] {
			images[p] = routeJSON(in.Images[p])
		}
	}
	policies := map[domain.QualityPolicy]modeladapter.PolicyRoute{}
	for _, q := range domain.QualityPolicies() {
		if in.Policies[q] != def.Policies[q] {
			policies[q] = in.Policies[q]
		}
	}
	if err := setKey(raw, keyImages, images); err != nil {
		return err
	}
	if err := setKey(raw, keyPolicies, policies); err != nil {
		return err
	}
	return s.committed(raw)
}

func setKey[K ~string, V any](raw settingsfile.Object, key string, entries map[K]V) error {
	if len(entries) == 0 {
		delete(raw, key)
		return nil
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	raw[key] = data
	return nil
}

// imageTargetJSON names Fast explicitly: an omitted "fast" would keep the default's value, so that a
// route could not switch it off.
type imageTargetJSON struct {
	Model domain.ModelName `json:"model"`
	Fast  bool             `json:"fast"`
}

type imageRouteJSON struct {
	Primary  imageTargetJSON `json:"primary"`
	Fallback imageTargetJSON `json:"fallback"`
}

func routeJSON(r modeladapter.ImageRoute) imageRouteJSON {
	return imageRouteJSON{
		Primary:  imageTargetJSON{Model: r.Primary.Model, Fast: r.Primary.Fast},
		Fallback: imageTargetJSON{Model: r.Fallback.Model, Fast: r.Fallback.Fast},
	}
}
