package service

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// AnalyticsService is Production Analytics (spec/spec.md §28): the MVP Success Metrics, computed
// from the local database only.
type AnalyticsService struct{ *core }

// AnalyticsView is the metrics screen: the metrics of one Project, or of every Project.
type AnalyticsView struct {
	// Project is nil for the metrics of every Project.
	Project *domain.Project
	Metrics domain.ProductionMetrics
}

// Metrics returns the metrics of the Project (GET /projects/:id/analytics), or, with an empty
// projectID, of every Project (GET /analytics).
func (s *AnalyticsService) Metrics(ctx context.Context, projectID string) (AnalyticsView, error) {
	var view AnalyticsView
	if projectID != "" {
		project, err := s.getProject(ctx, projectID)
		if err != nil {
			return AnalyticsView{}, err
		}
		view.Project = &project
	}
	m, err := s.analytics.Metrics(ctx, projectID)
	if err != nil {
		return AnalyticsView{}, notFound(err)
	}
	view.Metrics = m
	return view, nil
}
