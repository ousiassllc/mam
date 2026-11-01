// Package voicerefrepo persists the reference voice of a Character (F-20, F-29): the voice_reference column of
// the character rows of a Project and of a Series canon. The column is read with the rows (World, Canon);
// this package writes it. A change of the canon appends a series_version, like any other change of it.
// It is a sub-package of repository, which is at its line limit.
package voicerefrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Repository writes the voice_reference of character rows.
type Repository struct {
	db     *repository.DB
	series *repository.SeriesRepository
}

// New returns a Repository backed by db; series appends the canon versions.
func New(db *repository.DB, series *repository.SeriesRepository) *Repository {
	return &Repository{db: db, series: series}
}

// SetProject sets the reference voice of the Character row of the Project (a row of the trunk or of a Branch);
// nil removes it. It returns the one the row had (nil for none). A row that is not the Project's is
// repository.ErrNotFound.
func (r *Repository) SetProject(ctx context.Context, projectID, characterID string, ref *voiceref.Reference) (*voiceref.Reference, error) {
	var previous *voiceref.Reference
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		previous, err = replace(ctx, tx, `project_id`, projectID, characterID, ref)
		return err
	})
	return previous, wrap(err)
}

// SetSeries sets the reference voice of the canonical Character; nil removes it. The canon gets a new version
// and the Episodes that already copied the Character keep what they have. It returns the one the canon row
// had. An unknown Series or Character is repository.ErrNotFound.
func (r *Repository) SetSeries(ctx context.Context, seriesID, characterID string, ref *voiceref.Reference) (*voiceref.Reference, error) {
	var previous *voiceref.Reference
	err := r.series.EditCanon(ctx, seriesID, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		previous, err = replace(ctx, tx, `series_id`, seriesID, characterID, ref)
		return err
	})
	return previous, wrap(err)
}

// replace swaps the voice_reference of the row owned by owner (the column named by ownerColumn: a constant). It
// returns the previous one; ErrNotFound when the owner has no such row.
func replace(ctx context.Context, tx *sql.Tx, ownerColumn, owner, characterID string, ref *voiceref.Reference) (*voiceref.Reference, error) {
	previous, err := read(ctx, tx, ownerColumn, owner, characterID)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE character SET voice_reference = ? WHERE id = ?`, ref, characterID)
	return previous, err
}

// Project returns the reference voice of the Character row of the Project, nil when it has none. A row that is
// not the Project's is repository.ErrNotFound.
func (r *Repository) Project(ctx context.Context, projectID, characterID string) (*voiceref.Reference, error) {
	ref, err := read(ctx, r.db.Reader(), `project_id`, projectID, characterID)
	return ref, wrap(err)
}

// Series returns the reference voice of the canonical Character, nil when it has none. An unknown Series or
// Character is repository.ErrNotFound.
func (r *Repository) Series(ctx context.Context, seriesID, characterID string) (*voiceref.Reference, error) {
	ref, err := read(ctx, r.db.Reader(), `series_id`, seriesID, characterID)
	return ref, wrap(err)
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func read(ctx context.Context, q queryer, ownerColumn, owner, characterID string) (*voiceref.Reference, error) {
	var ref *voiceref.Reference
	err := q.QueryRowContext(ctx, `SELECT voice_reference FROM character WHERE id = ? AND `+ownerColumn+` = ?`, characterID, owner).Scan(&ref) //nolint:gosec // ownerColumn is a constant of this file
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return ref, err
}

// InUse reports whether a character row (of a Project, a Branch or the canon) refers to the file: a file that
// nothing refers to any more can be removed.
func (r *Repository) InUse(ctx context.Context, filePath string) (bool, error) {
	var n int
	err := r.db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM character WHERE json_extract(voice_reference, '$.file_path') = ?`, filePath).Scan(&n)
	return n > 0, wrap(err)
}

func wrap(err error) error {
	if err == nil || errors.Is(err, repository.ErrNotFound) {
		return err
	}
	return fmt.Errorf("voice reference: %w", err)
}
