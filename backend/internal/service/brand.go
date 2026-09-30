package service

import (
	"context"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// BrandService manages the Safety / Brand Constraints of a Project (F-23, UC-9). The constraints are
// enforced by the Prompt Planner, the AI Video Judge and Final QC, which read them themselves.
type BrandService struct {
	*core
	brands *repository.BrandRepository
}

// BrandView is the constraints of a Project with the Project.
type BrandView struct {
	Project     domain.Project
	Constraints domain.BrandConstraints
}

// BrandForm is the user's input for a new constraint. An empty Level is LOCK, the strict choice.
type BrandForm struct {
	Type  string
	Level string
	Text  string
}

// List returns the constraints of the Project.
func (s *BrandService) List(ctx context.Context, projectID string) (BrandView, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return BrandView{}, err
	}
	list, err := s.brands.List(ctx, projectID)
	if err != nil {
		return BrandView{}, err
	}
	return BrandView{Project: project, Constraints: list}, nil
}

// Add registers a constraint and returns the constraints of the Project. Invalid input is a
// *ValidationError.
func (s *BrandService) Add(ctx context.Context, projectID string, form BrandForm) (BrandView, error) {
	c := domain.BrandConstraint{
		ProjectID: projectID, Type: domain.BrandConstraintType(strings.TrimSpace(form.Type)),
		Level: domain.EnforcementLevel(strings.TrimSpace(form.Level)), Text: strings.TrimSpace(form.Text),
	}
	if c.Level == "" {
		c.Level = domain.EnforceLock
	}
	if msg := c.Validate(); msg != "" {
		field := "text"
		if !c.Type.Valid() {
			field = "type"
		} else if !c.Level.Valid() {
			field = "level"
		}
		return BrandView{}, &ValidationError{Field: field, Message: msg}
	}
	if _, err := s.brands.Create(ctx, c); err != nil {
		return BrandView{}, notFound(err)
	}
	return s.List(ctx, projectID)
}

// Delete removes a constraint and returns the constraints its Project has left.
func (s *BrandService) Delete(ctx context.Context, id string) (BrandView, error) {
	projectID, err := s.brands.Delete(ctx, id)
	if err != nil {
		return BrandView{}, notFound(err)
	}
	return s.List(ctx, projectID)
}
