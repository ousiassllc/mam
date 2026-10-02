package service

import (
	"context"
	"errors"
	"slices"
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

// Templates returns every Brand Template with its constraints, oldest first.
func (s *core) Templates(ctx context.Context) ([]domain.BrandTemplate, error) {
	return s.brandTemplates.List(ctx)
}

// TemplatesView is the Brand Templates next to the Project they are saved from and applied to.
type TemplatesView struct {
	ProjectID string
	Templates []domain.BrandTemplate
}

// TemplatesFor returns the Brand Templates for the settings screen of the Project.
func (s *BrandService) TemplatesFor(ctx context.Context, projectID string) (TemplatesView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return TemplatesView{}, err
	}
	list, err := s.Templates(ctx)
	if err != nil {
		return TemplatesView{}, err
	}
	return TemplatesView{ProjectID: projectID, Templates: list}, nil
}

// SaveTemplate stores the constraints the Project has now as a Brand Template with the name (POST
// /projects/:id/brand-templates). The template is a snapshot: later changes to the Project's
// constraints do not reach it. An invalid or taken name, or a Project without constraints, is a
// *ValidationError; an unknown Project is ErrNotFound.
func (s *BrandService) SaveTemplate(ctx context.Context, projectID, name string) (TemplatesView, error) {
	name, ok := domain.ValidateBrandTemplateName(name)
	if !ok {
		return TemplatesView{}, invalid("name", "テンプレート名は 1〜%d 文字で入力してください", domain.MaxBrandTemplateNameRunes)
	}
	_, err := s.brandTemplates.SaveFromProject(ctx, name, projectID)
	switch {
	case errors.Is(err, repository.ErrConflict):
		return TemplatesView{}, invalid("name", "この名前のテンプレートはすでにあります")
	case errors.Is(err, repository.ErrEmpty):
		return TemplatesView{}, invalid("name", "保存できる制約がありません。先に制約を登録してください")
	case err != nil:
		return TemplatesView{}, notFound(err)
	}
	return s.TemplatesFor(ctx, projectID)
}

// DeleteTemplate removes a Brand Template (DELETE /brand-templates/:id). Projects that used it keep
// their constraints. projectID is the settings screen the answer is rendered for: an unknown
// Project is ErrNotFound and deletes nothing.
func (s *BrandService) DeleteTemplate(ctx context.Context, projectID, templateID string) (TemplatesView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return TemplatesView{}, err
	}
	if err := s.brandTemplates.Delete(ctx, templateID); err != nil {
		return TemplatesView{}, notFound(err)
	}
	return s.TemplatesFor(ctx, projectID)
}

// ApplyTemplate copies the constraints of the template that the Project does not have yet into the
// Project and returns its constraints (POST /projects/:id/brand-templates/:templateId/apply). The
// copies are the Project's own: it can remove them freely, and changing or deleting the template
// later does not reach the Project. Unknown Project or template is ErrNotFound.
func (s *BrandService) ApplyTemplate(ctx context.Context, projectID, templateID string) (BrandView, error) {
	err := s.brandTemplates.Apply(ctx, templateID, projectID)
	if errors.Is(err, repository.ErrTemplateNotFound) {
		return BrandView{}, ErrNotFound
	}
	if err != nil {
		return BrandView{}, notFound(err)
	}
	return s.List(ctx, projectID)
}

// invalidTemplate is the error of a creation that names a Brand Template that does not exist.
func invalidTemplate() error {
	return invalid("brand_template_id", "選んだ Brand Template が見つかりません")
}

// checkBrandTexts returns ErrBrandViolation when one of the texts (a user's instruction or direction)
// contains a forbidden expression of a LOCK constraint of the Project. Spec §23: this runs before
// anything is generated or made (a Shot added, a Branch created, a paid Plan called). Blank texts
// are not looked at, and nothing is read when the repository is not wired.
func checkBrandTexts(ctx context.Context, brands *repository.BrandRepository, projectID string, texts ...string) error {
	if brands == nil || !slices.ContainsFunc(texts, func(t string) bool { return strings.TrimSpace(t) != "" }) {
		return nil
	}
	list, err := brands.List(ctx, projectID)
	if err != nil {
		return err
	}
	for _, t := range texts {
		if err := list.CheckText(t); err != nil {
			return err
		}
	}
	return nil
}
