package modeladapter

import (
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// routeFailure joins what every model of a route failed with (see resilience.RouteFailure): only a
// route whose models were all skipped by their breakers is an ErrCircuitOpen.
func routeFailure(failures []error) error {
	return resilience.RouteFailure(failures, func(err error) bool { return errors.Is(err, ErrCircuitOpen) })
}

func (a *Adapter) logFailover(m domain.ModelName, i, n int, err error) {
	if i+1 < n {
		a.logger.Warn("model failed, routing to the alternative", "model", m, "err", err)
	}
}
