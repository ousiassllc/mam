package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ErrEmpty means a Brand Template cannot be saved because the Project has no constraint.
var ErrEmpty = errors.New("repository: empty")

// ErrTemplateNotFound means the Brand Template to apply does not exist (the Project or Series being
// written to may exist: that is ErrNotFound).
var ErrTemplateNotFound = errors.New("repository: brand template not found")

// BrandTemplateRepository persists Brand Templates (F-23, M3, spec/architecture/er.md §2.4). All
// writes go through DB.Write.
type BrandTemplateRepository struct {
	db  *DB
	now func() time.Time
}

// NewBrandTemplateRepository returns a BrandTemplateRepository backed by db.
func NewBrandTemplateRepository(db *DB) *BrandTemplateRepository {
	return &BrandTemplateRepository{db: db, now: time.Now}
}

// templateNameFree returns ErrConflict when a template already has the name, ignoring case by Unicode
// simple folding (like the Reference Library's search and tags). The column's COLLATE NOCASE folds
// ASCII only, so it stays as a backstop and the comparison is made here.
func templateNameFree(ctx context.Context, tx *sql.Tx, name string) error {
	names, err := queryAll(ctx, tx, scanString, `SELECT name FROM brand_template`)
	if err != nil {
		return err
	}
	for _, taken := range names {
		if strings.EqualFold(taken, name) {
			return ErrConflict
		}
	}
	return nil
}

// SaveFromProject stores a snapshot of the Project's constraints as a template with the (already
// validated) name. An unknown Project is ErrNotFound, a Project without constraints ErrEmpty, a name
// that is taken (templateNameFree) ErrConflict. Later changes to the Project's constraints do not
// reach the template.
func (r *BrandTemplateRepository) SaveFromProject(ctx context.Context, name, projectID string) (domain.BrandTemplate, error) {
	t := domain.BrandTemplate{ID: domain.NewID(), Name: name, CreatedAt: r.now().UTC()}
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, projectID); err != nil {
			return err
		}
		if err := templateNameFree(ctx, tx, name); err != nil {
			return err
		}
		constraints, err := projectConstraints(ctx, tx, projectID)
		if err != nil {
			return err
		}
		if len(constraints) == 0 {
			return ErrEmpty
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO brand_template (id, name, created_at) VALUES (?, ?, ?)`,
			t.ID, t.Name, FormatTime(t.CreatedAt)); err != nil {
			return err
		}
		for _, c := range constraints {
			raw, err := json.Marshal(BrandValue{Text: c.Text})
			if err != nil {
				return fmt.Errorf("marshal brand constraint: %w", err)
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO brand_template_constraint (id, template_id, type, enforcement_level, value) VALUES (?, ?, ?, ?, ?)`,
				domain.NewID(), t.ID, c.Type, c.Level, string(raw)); err != nil {
				return err
			}
			t.Constraints = append(t.Constraints, domain.BrandConstraint{Type: c.Type, Level: c.Level, Text: c.Text})
		}
		return nil
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) || errors.Is(err, ErrEmpty) {
		return domain.BrandTemplate{}, err
	}
	if err != nil {
		return domain.BrandTemplate{}, fmt.Errorf("save brand template: %w", err)
	}
	return t, nil
}

// List returns every template with its constraints, oldest first. The constraints of all templates
// come from one query.
func (r *BrandTemplateRepository) List(ctx context.Context) ([]domain.BrandTemplate, error) {
	db := r.db.Reader()
	out, err := queryAll(ctx, db, scanBrandTemplate, `SELECT id, name, created_at FROM brand_template ORDER BY created_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("list brand templates: %w", err)
	}
	if len(out) == 0 {
		return out, nil
	}
	type templateConstraint struct {
		templateID string
		domain.BrandConstraint
	}
	rows, err := queryAll(ctx, db, func(row rowScanner) (templateConstraint, error) {
		var tc templateConstraint
		var value string
		if err := row.Scan(&tc.templateID, &tc.ID, &tc.Type, &tc.Level, &value); err != nil {
			return tc, fmt.Errorf("scan brand constraint: %w", err)
		}
		return tc, decodeBrandValue(&tc.BrandConstraint, value)
	}, `SELECT template_id, id, type, enforcement_level, value FROM brand_template_constraint ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("list brand constraints: %w", err)
	}
	byTemplate := make(map[string]domain.BrandConstraints, len(out))
	for _, tc := range rows {
		byTemplate[tc.templateID] = append(byTemplate[tc.templateID], tc.BrandConstraint)
	}
	for i := range out {
		out[i].Constraints = byTemplate[out[i].ID]
	}
	return out, nil
}

func scanBrandTemplate(row rowScanner) (domain.BrandTemplate, error) {
	var (
		t     domain.BrandTemplate
		added string
	)
	if err := row.Scan(&t.ID, &t.Name, &added); err != nil {
		return t, fmt.Errorf("scan brand template: %w", err)
	}
	var err error
	t.CreatedAt, err = ParseTime(added)
	return t, err
}

// Delete removes the template, or returns ErrNotFound. Projects that used it keep their constraints.
func (r *BrandTemplateRepository) Delete(ctx context.Context, id string) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM brand_template WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("delete brand template: %w", err)
	}
	return nil
}

// Apply copies the template's constraints that the Project does not have yet (same type, level and
// text) into the Project. An unknown Project is ErrNotFound, an unknown template ErrTemplateNotFound. The copies are the Project's
// own rows: it can change them freely, and the template is not consulted again.
func (r *BrandTemplateRepository) Apply(ctx context.Context, templateID, projectID string) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, projectID); err != nil {
			return err
		}
		return applyBrandTemplate(ctx, tx, templateID, projectID, FormatTime(r.now()))
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrTemplateNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("apply brand template: %w", err)
	}
	return nil
}

// applyBrandTemplate is Apply inside the transaction of another write (a new Project or Episode).
// An unknown template is ErrTemplateNotFound.
func applyBrandTemplate(ctx context.Context, tx *sql.Tx, templateID, projectID, now string) error {
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM brand_template WHERE id = ?`, templateID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTemplateNotFound
	}
	if err != nil {
		return err
	}
	tpl, err := templateConstraints(ctx, tx, templateID)
	if err != nil {
		return err
	}
	have, err := projectConstraints(ctx, tx, projectID)
	if err != nil {
		return err
	}
	for _, c := range tpl.MissingFrom(have) {
		raw, err := json.Marshal(BrandValue{Text: c.Text})
		if err != nil {
			return fmt.Errorf("marshal brand constraint: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO brand_constraint (id, project_id, type, enforcement_level, value, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			domain.NewID(), projectID, c.Type, c.Level, string(raw), now); err != nil {
			return fmt.Errorf("copy brand constraint: %w", err)
		}
	}
	return nil
}

func projectConstraints(ctx context.Context, q queryer, projectID string) (domain.BrandConstraints, error) {
	return readBrandConstraints(ctx, q, `SELECT id, type, enforcement_level, value FROM brand_constraint WHERE project_id = ? ORDER BY created_at, rowid`, projectID)
}

func templateConstraints(ctx context.Context, q queryer, templateID string) (domain.BrandConstraints, error) {
	return readBrandConstraints(ctx, q, `SELECT id, type, enforcement_level, value FROM brand_template_constraint WHERE template_id = ? ORDER BY rowid`, templateID)
}

// readBrandConstraints runs a query selecting id, type, enforcement_level and value. The rows carry no
// ProjectID and no time.
func readBrandConstraints(ctx context.Context, q queryer, query, arg string) (domain.BrandConstraints, error) {
	out, err := queryAll(ctx, q, func(row rowScanner) (domain.BrandConstraint, error) {
		var (
			c     domain.BrandConstraint
			value string
		)
		if err := row.Scan(&c.ID, &c.Type, &c.Level, &value); err != nil {
			return c, fmt.Errorf("scan brand constraint: %w", err)
		}
		return c, decodeBrandValue(&c, value)
	}, query, arg)
	if err != nil {
		return nil, fmt.Errorf("list brand constraints: %w", err)
	}
	return domain.BrandConstraints(out), nil
}

// decodeBrandValue fills the constraint's Text from the JSON value column.
func decodeBrandValue(c *domain.BrandConstraint, value string) error {
	var v BrandValue
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		return fmt.Errorf("decode brand constraint %s: %w", c.ID, err)
	}
	c.Text = v.Text
	return nil
}
