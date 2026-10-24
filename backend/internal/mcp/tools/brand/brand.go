// Package brand holds the MCP tools of the Brand / Safety Constraints and Brand Templates of a Project (the
// brand sections of the project settings screen, spec F-23).
package brand

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc.Brand}
	mcpkit.Read(r, "list_brand_constraints",
		"Project の Brand / Safety 制約の一覧 / List the Brand / Safety Constraints of a Project.",
		h.list)
	mcpkit.Write(r, "add_brand_constraint",
		"Brand / Safety 制約を登録する。level を省略すると lock（違反した Take は点数に関わらず不合格）/ Add a constraint (default level: lock).",
		h.add)
	mcpkit.Destroy(r, "delete_brand_constraint",
		"Brand / Safety 制約を削除する / Delete a constraint.",
		h.remove)
	mcpkit.Read(r, "list_brand_templates",
		"Brand Template（制約の組の保存済みスナップショット）の一覧。古い順 / List the Brand Templates, oldest first.",
		h.templates)
	mcpkit.Write(r, "save_brand_template",
		"Project の今の制約を、名前を付けて Brand Template として保存する（スナップショット。制約が 1 つもない Project や同名は validation）/ Save the Project's constraints as a Brand Template.",
		h.saveTemplate)
	mcpkit.Write(r, "apply_brand_template",
		"Brand Template の制約のうち Project がまだ持たないものを Project にコピーする（コピーは Project のもの）/ Copy the constraints of a template that the Project lacks into the Project.",
		h.applyTemplate)
	mcpkit.Destroy(r, "delete_brand_template",
		"Brand Template を削除する（使った Project の制約は残る）/ Delete a Brand Template; Projects keep their constraints.",
		h.deleteTemplate)
}

type handlers struct{ svc *service.BrandService }

// Constraint is one Brand / Safety Constraint.
type Constraint struct {
	ID        string    `json:"id"`
	Type      string    `json:"type" jsonschema:"brand_guideline, forbidden_expression, required_disclaimer, product_claim, logo_rule, character_restriction, asset_rights or platform_rule"`
	Level     string    `json:"level" jsonschema:"lock or advisory"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

func constraintsOf(cs domain.BrandConstraints) []Constraint {
	out := make([]Constraint, 0, len(cs))
	for _, c := range cs {
		out = append(out, Constraint{ID: c.ID, Type: string(c.Type), Level: string(c.Level), Text: c.Text, CreatedAt: c.CreatedAt})
	}
	return out
}

// ConstraintsOutput is the constraints of a Project.
type ConstraintsOutput struct {
	ProjectID   string       `json:"project_id"`
	Constraints []Constraint `json:"constraints"`
}

func outputOf(v service.BrandView, err error) (ConstraintsOutput, error) {
	return ConstraintsOutput{ProjectID: v.Project.ID, Constraints: constraintsOf(v.Constraints)}, err
}

// ProjectInput names the Project.
type ProjectInput struct {
	ProjectID string `json:"project_id"`
}

func (h handlers) list(ctx context.Context, in ProjectInput) (ConstraintsOutput, error) {
	return outputOf(h.svc.List(ctx, in.ProjectID))
}

// AddInput is a new constraint.
type AddInput struct {
	ProjectID string `json:"project_id"`
	Type      string `json:"type" jsonschema:"brand_guideline, forbidden_expression, required_disclaimer, product_claim, logo_rule, character_restriction, asset_rights or platform_rule"`
	Level     string `json:"level,omitempty" jsonschema:"lock or advisory。省略は lock"`
	Text      string `json:"text" jsonschema:"制約の内容（1000 文字まで）。forbidden_expression は 1 行に 1 語"`
}

func (h handlers) add(ctx context.Context, in AddInput) (ConstraintsOutput, error) {
	return outputOf(h.svc.Add(ctx, in.ProjectID, service.BrandForm{Type: in.Type, Level: in.Level, Text: in.Text}))
}

// DeleteInput names the constraint.
type DeleteInput struct {
	ConstraintID string `json:"constraint_id" jsonschema:"list_brand_constraints が返す id"`
}

func (h handlers) remove(ctx context.Context, in DeleteInput) (ConstraintsOutput, error) {
	return outputOf(h.svc.Delete(ctx, in.ConstraintID))
}
