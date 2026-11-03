// Package characterrepo adds a Character that was made by hand (#1001) to the World State of a Project or to the canon of a Series.
// The rows of a Character the planning made are written by the State Extractor and the Reference Pack; this is the way to have one
// without them. It is a sub-package of repository, which is at its line limit.
package characterrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// InWorld adds the Character to the World State of the Project (the trunk, or the Branch when branchID is set) and appends the
// resulting World State as a new version. c.ID is chosen by the caller. repository.ErrNotFound when the Project, or the Branch in it,
// does not exist; repository.ErrConflict when a Character of that name is there (domain.SameReferenceName: names are the key the
// State Extractor and the Reference Pack find a Character by).
func InWorld(ctx context.Context, world *repository.WorldRepository, projectID, branchID string, c domain.WorldCharacter) error {
	return world.EditWorld(ctx, projectID, branchID, func(ctx context.Context, tx *sql.Tx) error {
		if err := mustExist(ctx, tx, `SELECT 1 FROM project WHERE id = ?`, projectID); err != nil {
			return err
		}
		if branchID != "" {
			if err := mustExist(ctx, tx, `SELECT 1 FROM branch WHERE id = ? AND project_id = ?`, branchID, projectID); err != nil {
				return err
			}
		}
		if err := refuseUsedName(ctx, tx, c.Name, `SELECT name FROM character WHERE project_id = ? AND branch_id IS ?`, projectID, nullIfEmpty(branchID)); err != nil {
			return err
		}
		return insert(ctx, tx, insertInWorld, c, projectID, nullIfEmpty(branchID))
	})
}

// InCanon adds the Character to the canon of the Series and appends the canon as a new version; Episodes that already copied the canon
// keep what they have. Emotion, Position and CurrentAction are not canon and are not stored. repository.ErrNotFound when the Series does
// not exist; repository.ErrConflict when a canonical Character of that name is there.
func InCanon(ctx context.Context, series *repository.SeriesRepository, seriesID string, c domain.WorldCharacter) error {
	return series.EditCanon(ctx, seriesID, func(ctx context.Context, tx *sql.Tx) error {
		if err := mustExist(ctx, tx, `SELECT 1 FROM series WHERE id = ?`, seriesID); err != nil {
			return err
		}
		if err := refuseUsedName(ctx, tx, c.Name, `SELECT name FROM character WHERE series_id = ?`, seriesID); err != nil {
			return err
		}
		return insert(ctx, tx, insertInCanon, c, seriesID)
	})
}

// mustExist is repository.ErrNotFound when the query (SELECT 1 ... WHERE ...) finds no row.
func mustExist(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	var one int
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	} else if err != nil {
		return err
	}
	return nil
}

// refuseUsedName is repository.ErrConflict when one of the names the query selects is the name (domain.SameReferenceName).
func refuseUsedName(ctx context.Context, tx *sql.Tx, name, query string, args ...any) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var used string
		if err := rows.Scan(&used); err != nil {
			return err
		}
		if domain.SameReferenceName(used, name) {
			return repository.ErrConflict
		}
	}
	return rows.Err()
}

// DraftsInWorld adds the Characters to the World State of the trunk of the Project in one step, as one new version, but only when the
// trunk has no Character at all (a Character the planning made, one made by hand, or a draft made before): it reports whether it
// added them. This is what makes the drafts of the attached images (#1022) not grow when the planning stops again. The caller marks the
// rows (Draft) and chooses their IDs and names. repository.ErrNotFound when the Project does not exist.
func DraftsInWorld(ctx context.Context, world *repository.WorldRepository, projectID string, cs []domain.WorldCharacter) (bool, error) {
	added := false
	err := world.EditWorld(ctx, projectID, "", func(ctx context.Context, tx *sql.Tx) error {
		if err := mustExist(ctx, tx, `SELECT 1 FROM project WHERE id = ?`, projectID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM character WHERE project_id = ? AND branch_id IS NULL`, projectID).Scan(&n); err != nil {
			return err
		}
		if n > 0 || len(cs) == 0 {
			return errNothingToAdd
		}
		for _, c := range cs {
			if err := insert(ctx, tx, insertInWorld, c, projectID, nil); err != nil {
				return err
			}
		}
		added = true
		return nil
	})
	if errors.Is(err, errNothingToAdd) {
		return false, nil
	}
	return added, err
}

// errNothingToAdd stops EditWorld before it appends a version: the rollback of its transaction is the way out of it.
var errNothingToAdd = errors.New("characterrepo: nothing to add")

// The two INSERTs differ in the columns that say whose the row is (the Project and its Branch, or the Series).
const (
	insertInWorld = `INSERT INTO character (project_id, branch_id, id, name, hairstyle, wardrobe, voice, personality, face_reference_asset_id, body_reference_asset_id, draft)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	insertInCanon = `INSERT INTO character (series_id, id, name, hairstyle, wardrobe, voice, personality, face_reference_asset_id, body_reference_asset_id, draft)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
)

// insert writes the row with the query; ownerArgs are the values of its first columns.
func insert(ctx context.Context, tx *sql.Tx, query string, c domain.WorldCharacter, ownerArgs ...any) error {
	args := append(ownerArgs, c.ID, c.Name, nullIfEmpty(c.Hairstyle), nullIfEmpty(c.Wardrobe), nullIfEmpty(c.Voice), nullIfEmpty(c.Personality),
		nullIfEmpty(c.FaceReferenceAssetID), nullIfEmpty(c.BodyReferenceAssetID), c.Draft)
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert character: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
