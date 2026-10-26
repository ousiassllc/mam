package configcheck

import (
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// validateLocal checks the registered endpoints and the models defined on them. Their keys are checked
// in order, so the joined error lists the problems in the same order on every call.
func validateLocal(c modeladapter.Config) []error {
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(c.Endpoints)) {
		errs = append(errs, localcfg.ValidateEndpoint(name, c.Endpoints[name])...)
	}
	for _, key := range slices.Sorted(maps.Keys(c.LocalModels)) {
		errs = append(errs, localcfg.ValidateModel(key, c.LocalModels[key], c.Endpoints)...)
	}
	return errs
}

// isLocal reports whether m names a model defined on a registered endpoint.
func isLocal(c modeladapter.Config, m domain.ModelName) bool {
	_, _, ok := c.LocalModel(m)
	return ok
}
