// Package ollamamodels is the short list of Ollama models that mam fetches (spec/architecture/model-manager/local-runtime.md §19.2): a model of the registry of Ollama with its
// layers (the sizes and digests of the registry's manifest on the day it was checked), the license of the model and the answer for commercial use. It is a list of names, not
// a pin: the tags of the registry can be moved, so what a pull brings can differ from what is recorded here, and mam only compares the manifest it got with the record afterwards.
// Nothing outside the list is fetched: a name that is not in it, is not of the namespace "library" or has a tag with "cloud" (a model that runs on the servers of Ollama and sends the
// prompt there) is refused before any request. It is a package of its own because the directory of the catalog is at its line budget.
package ollamamodels

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
)

// The kinds of the layers of a model that the registry names (the media type "application/vnd.ollama.image.<kind>"). KindModel is the weights.
const (
	KindModel     = "model"
	KindProjector = "projector"
	KindDraft     = "draft"
)

// Layer is one blob of the manifest: its kind, its digest ("sha256:<64 hex>") and its size in bytes.
type Layer struct {
	Kind   string `json:"kind"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// Model is one model of the list. Name is "<name>:<tag>" of the namespace library (the one the user types: "gemma4:12b").
type Model struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	// Layers are the layers of the manifest; ConfigBytes the size of its config blob; TotalBytes what a pull transfers (the layers and the config).
	Layers      []Layer `json:"layers"`
	ConfigBytes int64   `json:"config_bytes"`
	TotalBytes  int64   `json:"total_bytes"`
	// License is the answer for commercial use and the license of the model (the text of the registry's license layer); Summary says what was read.
	License        modellicense.Info `json:"license"`
	LicenseSummary string            `json:"license_summary"`
	// CheckedAt is the date (YYYY-MM-DD) the manifest and the license were read.
	CheckedAt string `json:"checked_at"`
}

// ModelLayer is the layer that holds the weights: the one a pull is checked by.
func (m Model) ModelLayer() Layer {
	for _, l := range m.Layers {
		if l.Kind == KindModel {
			return l
		}
	}
	return Layer{}
}

// ErrName is a name that cannot be fetched: it is not "<name>:<tag>" of the namespace library, or its tag names a cloud model.
var ErrName = errors.New("ollamamodels: not a name of a model of the library")

var (
	namePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}:[a-z0-9][a-z0-9._-]{0,63}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// CheckName reports whether name is the form that may be fetched: "<name>:<tag>" without a namespace (so the namespace is library) and with a tag that does not
// name a cloud model. It does not say the model is in the list (Find does).
func CheckName(name string) error {
	_, tag, _ := strings.Cut(name, ":")
	if !namePattern.MatchString(name) || strings.Contains(tag, "cloud") {
		return ErrName
	}
	return nil
}

// Find is the model of the list called name, or false (also for a name that CheckName refuses).
func Find(models []Model, name string) (Model, bool) {
	if CheckName(name) != nil {
		return Model{}, false
	}
	i := slices.IndexFunc(models, func(m Model) bool { return m.Name == name })
	if i < 0 {
		return Model{}, false
	}
	return models[i], true
}

// Check validates the list: every model has a fetchable name that is used once, a name for the screen, a model layer with a digest, layer sizes that add up to the total, a check date and a license
// (which has to be one of the catalog's, commercial answer included).
func Check(models []Model) []error {
	var errs []error
	seen := map[string]bool{}
	for _, m := range models {
		where := "ollama model " + m.Name
		switch {
		case CheckName(m.Name) != nil:
			errs = append(errs, fmt.Errorf("%s: the name must be <name>:<tag> of the library, without \"cloud\"", where))
		case seen[m.Name]:
			errs = append(errs, fmt.Errorf("%s: duplicate name", where))
		case m.DisplayName == "" || strings.TrimSpace(m.Description) == "" || strings.TrimSpace(m.LicenseSummary) == "":
			errs = append(errs, fmt.Errorf("%s: display_name, description and license_summary are required", where))
		}
		seen[m.Name] = true
		errs = append(errs, checkLayers(where, m)...)
		if err := m.License.Check(); err != nil {
			errs = append(errs, fmt.Errorf("%s: license: %w", where, err))
		}
	}
	return errs
}

func checkLayers(where string, m Model) []error {
	var errs []error
	total, models := m.ConfigBytes, 0
	for _, l := range m.Layers {
		if !digestPattern.MatchString(l.Digest) || l.Size <= 0 || l.Kind == "" {
			errs = append(errs, fmt.Errorf("%s: layer %q needs a kind, a sha256 digest and a size", where, l.Kind))
		}
		if l.Kind == KindModel {
			models++
		}
		total += l.Size
	}
	if models != 1 {
		errs = append(errs, fmt.Errorf("%s: exactly one layer must be of the kind %q, has %d", where, KindModel, models))
	}
	if m.ConfigBytes <= 0 || m.TotalBytes != total {
		errs = append(errs, fmt.Errorf("%s: total_bytes %d must be the sum of the layers and the config (%d)", where, m.TotalBytes, total))
	}
	return errs
}
