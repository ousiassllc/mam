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

// ErrShotBusy means the Shot is being generated, so its Reference Pack cannot be changed now.
var ErrShotBusy = errors.New("repository: shot is being generated")

// ReferenceRepository persists the Reference Library of a Project (spec/spec.md §10.1): the
// reference_item rows and the pinning of their images to the Reference Pack of a Shot. All writes
// go through DB.Write.
type ReferenceRepository struct {
	db  *DB
	now func() time.Time
}

// NewReferenceRepository returns a ReferenceRepository backed by db.
func NewReferenceRepository(db *DB) *ReferenceRepository {
	return &ReferenceRepository{db: db, now: time.Now}
}

// ReferenceFilter narrows a Library listing. Every set field must match. Query is a part of the name
// or of a tag, Tag is one whole tag, both ignoring case (domain.ReferenceItem.Matches).
type ReferenceFilter struct {
	Query string
	Kind  domain.ReferenceKind
	Tag   string
}

const referenceSelect = `SELECT id, project_id, asset_id, kind, name, tags, source, COALESCE(source_shot_id, ''), created_at FROM reference_item`

func scanReferenceItem(row rowScanner) (domain.ReferenceItem, error) {
	var (
		it   domain.ReferenceItem
		tags string
		at   string
	)
	if err := row.Scan(&it.ID, &it.ProjectID, &it.AssetID, &it.Kind, &it.Name, &tags, &it.Source, &it.SourceShotID, &at); err != nil {
		return domain.ReferenceItem{}, err
	}
	if err := json.Unmarshal([]byte(tags), &it.Tags); err != nil {
		return domain.ReferenceItem{}, fmt.Errorf("reference tags: %w", err)
	}
	t, err := ParseTime(at)
	if err != nil {
		return domain.ReferenceItem{}, err
	}
	it.CreatedAt = t
	return it, nil
}

// List returns the Library of the Project matching the filter, newest first.
func (r *ReferenceRepository) List(ctx context.Context, projectID string, f ReferenceFilter) ([]domain.ReferenceItem, error) {
	q := referenceSelect + ` WHERE project_id = ?`
	args := []any{projectID}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, string(f.Kind))
	}
	q += ` ORDER BY created_at DESC, id DESC`
	all, err := queryAll(ctx, r.db.Reader(), scanReferenceItem, q, args...) //nolint:gosec // q is assembled from constants; only the arguments are data
	if err != nil {
		return nil, fmt.Errorf("list reference library: %w", err)
	}
	items := []domain.ReferenceItem{}
	for _, it := range all {
		// Query and Tag are matched here: SQLite folds the case of ASCII only.
		if it.Matches(f.Query, f.Tag) {
			items = append(items, it)
		}
	}
	return items, nil
}

// Get returns an entry of the Project's Library or ErrNotFound.
func (r *ReferenceRepository) Get(ctx context.Context, projectID, id string) (domain.ReferenceItem, error) {
	return getReferenceItem(ctx, r.db.Reader(), projectID, id)
}

func getReferenceItem(ctx context.Context, q queryer, projectID, id string) (domain.ReferenceItem, error) {
	rows, err := q.QueryContext(ctx, referenceSelect+` WHERE project_id = ? AND id = ?`, projectID, id)
	if err != nil {
		return domain.ReferenceItem{}, fmt.Errorf("get reference item: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return domain.ReferenceItem{}, err
		}
		return domain.ReferenceItem{}, ErrNotFound
	}
	return scanReferenceItem(rows)
}

// Add registers an uploaded image: the asset (type reference) and its Library entry are written
// together. An unknown Project is ErrNotFound. item.ID and item.AssetID are set by the caller.
func (r *ReferenceRepository) Add(ctx context.Context, item domain.ReferenceItem, asset domain.Asset) error {
	now := FormatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, item.ProjectID); err != nil {
			return err
		}
		if err := insertAsset(ctx, tx, asset, now); err != nil {
			return err
		}
		return insertReferenceItem(ctx, tx, item, now)
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("add reference item: %w", err)
	}
	return nil
}

func insertReferenceItem(ctx context.Context, tx *sql.Tx, it domain.ReferenceItem, now string) error {
	tags, err := json.Marshal(append([]string{}, it.Tags...))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO reference_item (id, project_id, asset_id, kind, name, tags, source, source_shot_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.ID, it.ProjectID, it.AssetID, it.Kind, it.Name, string(tags), it.Source, nullIfEmpty(it.SourceShotID), now)
	if err != nil {
		return fmt.Errorf("insert reference item: %w", err)
	}
	return nil
}

// Delete removes an entry from the Library. The image and the references Shots already hold stay.
// An entry that is not in the Project is ErrNotFound.
func (r *ReferenceRepository) Delete(ctx context.Context, projectID, id string) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM reference_item WHERE project_id = ? AND id = ?`, projectID, id)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("delete reference item: %w", err)
	}
	return nil
}

// Pin adds the Library image to the Reference Pack of the Shot: a reference of the same kind and
// name is replaced, and the new one records the entry in library_item. An unknown Shot, or an
// entry of another Project, is ErrNotFound; a Shot that is being generated is ErrShotBusy.
func (r *ReferenceRepository) Pin(ctx context.Context, shotID, itemID string) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var (
			raw, state, projectID string
		)
		err := tx.QueryRowContext(ctx,
			`SELECT sh.shot_spec, sh.state, s.project_id FROM shot sh JOIN scene s ON s.id = sh.scene_id WHERE sh.id = ?`, shotID).
			Scan(&raw, &state, &projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !domain.ShotState(state).CanPinReference() {
			return ErrShotBusy
		}
		item, err := getReferenceItem(ctx, tx, projectID, itemID)
		if err != nil {
			return err
		}
		spec := json.RawMessage(raw)
		refs := domain.Shot{ShotSpec: spec}.References()
		kept := make([]domain.Reference, 0, len(refs)+1)
		pinned := domain.Reference{Kind: item.Kind, Name: item.Name, AssetID: item.AssetID, LibraryItem: item.ID}
		for _, ref := range refs {
			if domain.SameReference(ref, pinned) {
				continue
			}
			kept = append(kept, ref)
		}
		kept = append(kept, pinned)
		next, err := domain.WithReferences(spec, kept)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE shot SET shot_spec = ? WHERE id = ?`, string(next), shotID)
		return err
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrShotBusy) {
		return err
	}
	if err != nil {
		return fmt.Errorf("pin reference: %w", err)
	}
	return nil
}
