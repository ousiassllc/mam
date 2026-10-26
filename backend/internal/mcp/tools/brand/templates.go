package brand

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// Template is a Brand Template with the constraints it holds.
type Template struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Constraints []Constraint `json:"constraints"`
	CreatedAt   time.Time    `json:"created_at"`
}

// TemplatesOutput is the Brand Templates, oldest first.
type TemplatesOutput struct {
	ProjectID string     `json:"project_id"`
	Templates []Template `json:"templates"`
}

func templatesOf(v service.TemplatesView, err error) (TemplatesOutput, error) {
	out := TemplatesOutput{ProjectID: v.ProjectID, Templates: make([]Template, 0, len(v.Templates))}
	for _, t := range v.Templates {
		out.Templates = append(out.Templates, Template{ID: t.ID, Name: t.Name, Constraints: constraintsOf(t.Constraints), CreatedAt: t.CreatedAt})
	}
	return out, err
}

func (h handlers) templates(ctx context.Context, in ProjectInput) (TemplatesOutput, error) {
	return templatesOf(h.svc.TemplatesFor(ctx, in.ProjectID))
}

// SaveTemplateInput is the name of the new template.
type SaveTemplateInput struct {
	ProjectID string `json:"project_id" jsonschema:"制約を取り出す Project"`
	Name      string `json:"name" jsonschema:"テンプレート名（1〜100 文字。同名は不可）"`
}

func (h handlers) saveTemplate(ctx context.Context, in SaveTemplateInput) (TemplatesOutput, error) {
	return templatesOf(h.svc.SaveTemplate(ctx, in.ProjectID, in.Name))
}

// TemplateInput names a template and the Project it is used with.
type TemplateInput struct {
	ProjectID  string `json:"project_id"`
	TemplateID string `json:"template_id" jsonschema:"list_brand_templates が返す id"`
}

func (h handlers) applyTemplate(ctx context.Context, in TemplateInput) (ConstraintsOutput, error) {
	return outputOf(h.svc.ApplyTemplate(ctx, in.ProjectID, in.TemplateID))
}

func (h handlers) deleteTemplate(ctx context.Context, in TemplateInput) (TemplatesOutput, error) {
	return templatesOf(h.svc.DeleteTemplate(ctx, in.ProjectID, in.TemplateID))
}
