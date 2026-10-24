package gentest

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// Models lists the models of the media ("" for both), each with what it can do and why it cannot be chosen now.
func (s *Service) Models(_ context.Context, media string) []modelcatalog.Info {
	return s.catalog.Models(media)
}

// Model returns one model of the media, or gterr.ErrNotFound.
func (s *Service) Model(_ context.Context, media, id string) (modelcatalog.Info, error) {
	if m, ok := s.catalog.Model(media, id); ok {
		return m, nil
	}
	return modelcatalog.Info{}, gterr.ErrNotFound
}

// Limits returns the caps of one batch from models.json: the number of results and the estimated cost in USD (0: no cap).
func (s *Service) Limits() (maxItems int, maxCostUSD float64) {
	g := s.Adapter.Config().Gentest
	return g.Items(), g.CostUSD()
}
