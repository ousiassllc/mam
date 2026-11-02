// Package reciperepo persists the recipes of the generation test (table gentest_recipe, Issue #977): a named set of the settings of an image, to
// be used again. params is the JSON the service reads (service/gentest/recipe); this package knows only that it is JSON. It is a sub-package of
// repository, which is at its line limit.
package reciperepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Recipe is one row of gentest_recipe.
type Recipe struct {
	ID        string
	Name      string
	Params    json.RawMessage
	Easy      bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Repository persists the recipes. All writes go through DB.Write.
type Repository struct{ db *repository.DB }

// New returns a Repository backed by db.
func New(db *repository.DB) *Repository { return &Repository{db: db} }

const columns = `id, name, params, easy, created_at, updated_at`

// Create inserts a recipe. A name that is taken (ignoring case) is repository.ErrConflict.
func (r *Repository) Create(ctx context.Context, x Recipe) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO gentest_recipe (`+columns+`) VALUES (?, ?, ?, ?, ?, ?)`,
			x.ID, x.Name, string(x.Params), boolInt(x.Easy), repository.FormatTime(x.CreatedAt), repository.FormatTime(x.UpdatedAt))
		return conflict("insert gentest recipe", err)
	})
}

// Update replaces the name and the easy flag of the recipe (the params are kept: a recipe is saved once and named again, not edited). An unknown ID is
// repository.ErrNotFound, a name that is taken repository.ErrConflict.
func (r *Repository) Update(ctx context.Context, x Recipe) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE gentest_recipe SET name = ?, easy = ?, updated_at = ? WHERE id = ?`,
			x.Name, boolInt(x.Easy), repository.FormatTime(x.UpdatedAt), x.ID)
		if err != nil {
			return conflict("update gentest recipe", err)
		}
		return affected(res)
	})
}

// Delete removes the recipe. An unknown ID is repository.ErrNotFound.
func (r *Repository) Delete(ctx context.Context, id string) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM gentest_recipe WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("delete gentest recipe: %w", err)
		}
		return affected(res)
	})
}

// List returns every recipe by name.
func (r *Repository) List(ctx context.Context) ([]Recipe, error) {
	rows, err := r.db.Reader().QueryContext(ctx, `SELECT `+columns+` FROM gentest_recipe ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list gentest recipes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Recipe
	for rows.Next() {
		x, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Get returns the recipe with the ID, or repository.ErrNotFound.
func (r *Repository) Get(ctx context.Context, id string) (Recipe, error) {
	x, err := scan(r.db.Reader().QueryRowContext(ctx, `SELECT `+columns+` FROM gentest_recipe WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Recipe{}, repository.ErrNotFound
	}
	return x, err
}

type rowScanner interface{ Scan(dest ...any) error }

func scan(s rowScanner) (Recipe, error) {
	var x Recipe
	var params, created, updated string
	var easy int
	if err := s.Scan(&x.ID, &x.Name, &params, &easy, &created, &updated); err != nil {
		return Recipe{}, err
	}
	x.Params, x.Easy = json.RawMessage(params), easy == 1
	var err error
	if x.CreatedAt, err = repository.ParseTime(created); err != nil {
		return Recipe{}, err
	}
	x.UpdatedAt, err = repository.ParseTime(updated)
	return x, err
}

func affected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// conflict maps a UNIQUE violation (the name) to repository.ErrConflict.
func conflict(what string, err error) error {
	if err == nil {
		return nil
	}
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE {
		return repository.ErrConflict
	}
	return fmt.Errorf("%s: %w", what, err)
}
