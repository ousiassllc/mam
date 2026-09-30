package service

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// TraceService shows the Generation Traceability of a Take (F-27, spec/spec.md §29.3).
type TraceService struct {
	traces *repository.TraceRepository
}

// Take returns everything that led to the Take (GET /takes/:id/trace).
func (s *TraceService) Take(ctx context.Context, takeID string) (domain.TakeTrace, error) {
	tr, err := s.traces.TakeTrace(ctx, takeID)
	if err != nil {
		return domain.TakeTrace{}, notFound(err)
	}
	return tr, nil
}
