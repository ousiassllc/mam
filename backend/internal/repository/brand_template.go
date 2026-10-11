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
	rows, err := tx.QueryContext(ctx, `SELECT name FROM brand_template`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var taken string
		if err := rows.Scan(&taken); err != nil {
			return err
		}
		if strings.EqualFold(taken, name) {
			return ErrConflict
		}
	}
	return rows.Err()
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
			t.ID, t.Name, formatTime(t.CreatedAt)); err != nil {
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

// List returns every template with its constraints, oldest first.
func (r *BrandTemplateRepository) List(ctx context.Context) ([]domain.BrandTemplate, error) {
	rows, err := r.db.Reader().QueryContext(ctx, `SELECT id, name, created_at FROM brand_template ORDER BY created_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("list brand templates: %w", err)
	}
	var out []domain.BrandTemplate
	for rows.Next() {
		var (
			t     domain.BrandTemplate
			added string
		)
		if err := rows.Scan(&t.ID, &t.Name, &added); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan brand template: %w", err)
		}
		if t.CreatedAt, err = ParseTime(added); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	for i := range out {
		if out[i].Constraints, err = templateConstraints(ctx, r.db.Reader(), out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
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
		return applyBrandTemplate(ctx, tx, templateID, projectID, formatTime(r.now()))
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrTemplateNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("apply brand template: %w", err)
	}
	return nil
}

// querier is what *sql.DB and *sql.Tx share.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
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

func projectConstraints(ctx context.Context, q querier, projectID string) (domain.BrandConstraints, error) {
	return readBrandConstraints(ctx, q, `SELECT id, type, enforcement_level, value FROM brand_constraint WHERE project_id = ? ORDER BY created_at, rowid`, projectID)
}

func templateConstraints(ctx context.Context, q querier, templateID string) (domain.BrandConstraints, error) {
	return readBrandConstraints(ctx, q, `SELECT id, type, enforcement_level, value FROM brand_template_constraint WHERE template_id = ? ORDER BY rowid`, templateID)
}

// readBrandConstraints runs a query selecting id, type, enforcement_level and value. The rows carry no
// ProjectID and no time.
func readBrandConstraints(ctx context.Context, q querier, query, arg string) (domain.BrandConstraints, error) {
	rows, err := q.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("list brand constraints: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out domain.BrandConstraints
	for rows.Next() {
		var (
			c     domain.BrandConstraint
			value string
		)
		if err := rows.Scan(&c.ID, &c.Type, &c.Level, &value); err != nil {
			return nil, fmt.Errorf("scan brand constraint: %w", err)
		}
		var v BrandValue
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return nil, fmt.Errorf("decode brand constraint %s: %w", c.ID, err)
		}
		c.Text = v.Text
		out = append(out, c)
	}
	return out, rows.Err()
}
