package configcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/overlay"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Read returns DefaultConfig with the settings of the JSON file applied on top. An empty
// path selects the defaults. Unknown keys are rejected so a typo does not silently change nothing.
// The result is not checked: Load does that.
func Read(path string) (modeladapter.Config, error) {
	cfg := modeladapter.DefaultConfig()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the operator chooses the settings file
	if err != nil {
		return modeladapter.Config{}, fmt.Errorf("read model config: %w", err)
	}
	// The maps of entries are decoded per entry on top of the default entry, so that an entry that
	// names only some of its fields keeps the defaults of the others (encoding/json would rebuild the
	// whole entry from the zero value): vendors, models, images, policies and routing.weights. The
	// embedded Config shares the default maps; Routing shadows its Config field so that the weights
	// stay raw until they are overlaid.
	file := struct {
		modeladapter.Config
		Vendors  map[modeladapter.Vendor]json.RawMessage       `json:"vendors"`
		Models   map[domain.ModelName]json.RawMessage          `json:"models"`
		Images   map[modeladapter.ImagePurpose]json.RawMessage `json:"images"`
		Policies map[domain.QualityPolicy]json.RawMessage      `json:"policies"`
		Routing  struct {
			modeladapter.RoutingConfig
			Weights map[domain.QualityPolicy]json.RawMessage `json:"weights"`
		} `json:"routing"`
	}{Config: cfg}
	file.Routing.RoutingConfig = cfg.Routing
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return modeladapter.Config{}, fmt.Errorf("decode model config %s: %w", path, err)
	}
	cfg = file.Config
	cfg.Routing = file.Routing.RoutingConfig
	for _, err := range []error{
		overlay.Entries(cfg.Vendors, file.Vendors), overlay.Entries(cfg.Models, file.Models),
		overlay.Entries(cfg.Images, file.Images), overlay.Entries(cfg.Policies, file.Policies),
		overlay.Entries(cfg.Routing.Weights, file.Routing.Weights),
	} {
		if err != nil {
			return modeladapter.Config{}, fmt.Errorf("decode model config %s: %w", path, err)
		}
	}
	return cfg, nil
}
