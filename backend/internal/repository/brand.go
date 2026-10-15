package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// BrandRepository persists the Safety / Brand Constraints of a Project (F-23,
// spec/architecture/er.md §2.4). All writes go through DB.Write.
type BrandRepository struct {
	db  *DB
	now func() time.Time
}

// NewBrandRepository returns a BrandRepository backed by db.
func NewBrandRepository(db *DB) *BrandRepository { return &BrandRepository{db: db, now: time.Now} }

// BrandValue is brand_constraint.value.
type BrandValue struct {
	Text string `json:"text"`
}

// Create registers a constraint of the Project, or returns ErrNotFound when the Project does not exist.
func (r *BrandRepository) Create(ctx context.Context, c domain.BrandConstraint) (domain.BrandConstraint, error) {
	raw, err := json.Marshal(BrandValue{Text: c.Text})
	if err != nil {
		return domain.BrandConstraint{}, fmt.Errorf("marshal brand constraint: %w", err)
	}
	c.ID, c.CreatedAt = domain.NewID(), r.now().UTC()
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, c.ProjectID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO brand_constraint (id, project_id, type, enforcement_level, value, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			c.ID, c.ProjectID, c.Type, c.Level, string(raw), FormatTime(c.CreatedAt))
		return err
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.BrandConstraint{}, err
		}
		return domain.BrandConstraint{}, fmt.Errorf("create brand constraint: %w", err)
	}
	return c, nil
}

// Delete removes the constraint and returns the ID of its Project, or ErrNotFound.
func (r *BrandRepository) Delete(ctx context.Context, id string) (string, error) {
	var projectID string
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT project_id FROM brand_constraint WHERE id = ?`, id).Scan(&projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM brand_constraint WHERE id = ?`, id)
		return err
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", err
		}
		return "", fmt.Errorf("delete brand constraint: %w", err)
	}
	return projectID, nil
}

// List returns the constraints of the Project in the order they were registered.
func (r *BrandRepository) List(ctx context.Context, projectID string) (domain.BrandConstraints, error) {
	out, err := queryAll(ctx, r.db.Reader(), func(row rowScanner) (domain.BrandConstraint, error) {
		var (
			c            domain.BrandConstraint
			value, added string
		)
		if err := row.Scan(&c.ID, &c.ProjectID, &c.Type, &c.Level, &value, &added); err != nil {
			return c, fmt.Errorf("scan brand constraint: %w", err)
		}
		if err := decodeBrandValue(&c, value); err != nil {
			return c, err
		}
		var err error
		c.CreatedAt, err = ParseTime(added)
		return c, err
	}, `SELECT id, project_id, type, enforcement_level, value, created_at FROM brand_constraint
		 WHERE project_id = ? ORDER BY created_at, rowid`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list brand constraints: %w", err)
	}
	return domain.BrandConstraints(out), nil
}
