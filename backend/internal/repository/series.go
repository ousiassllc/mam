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

// ErrNotInSeries means the Project is not an Episode of the Series it was named as the previous
// Episode of.
var ErrNotInSeries = errors.New("repository: project is not an episode of the series")

// SeriesRepository persists Series (F-29, spec/architecture/er.md §4): the canonical Characters,
// Products, Locations and Style, their append-only history (series_version), and the Episodes
// that inherit them. All writes go through DB.Write.
type SeriesRepository struct {
	db    *DB
	now   func() time.Time
	world *WorldRepository
}

// NewSeriesRepository returns a SeriesRepository backed by db.
func NewSeriesRepository(db *DB) *SeriesRepository {
	return &SeriesRepository{db: db, now: time.Now, world: NewWorldRepository(db)}
}

// Create makes a Series named name. With fromProjectID the Character, Product and Location rows
// of that Project become the canon (only the canonical fields, see domain.CanonCharacter), its
// Brief Style and first style reference become the canon Style, and the Project becomes Episode
// 1 with its rows pointing at the canon rows through origin_id. The canon is version 1. An
// unknown Project is ErrNotFound; one that already belongs to a Series is ErrConflict.
func (r *SeriesRepository) Create(ctx context.Context, name, fromProjectID string) (domain.Series, error) {
	id := domain.NewID()
	now := FormatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var style, styleAsset string
		if fromProjectID != "" {
			var seriesID sql.NullString
			err := tx.QueryRowContext(ctx, `SELECT series_id FROM project WHERE id = ?`, fromProjectID).Scan(&seriesID)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if seriesID.Valid {
				return ErrConflict
			}
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(json_extract(content, '$.style'), '') FROM brief WHERE project_id = ?`, fromProjectID).Scan(&style); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			err = tx.QueryRowContext(ctx, `SELECT asset_id FROM reference_item WHERE project_id = ? AND kind = 'style' ORDER BY created_at, id LIMIT 1`, fromProjectID).Scan(&styleAsset)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO series (id, name, style, style_reference_asset_id, created_at) VALUES (?, ?, ?, ?, ?)`,
			id, name, style, nullIfEmpty(styleAsset), now); err != nil {
			return fmt.Errorf("insert series: %w", err)
		}
		if fromProjectID == "" {
			_, err := r.appendVersion(ctx, tx, id, now)
			return err
		}
		if err := registerCanon(ctx, tx, id, fromProjectID); err != nil {
			return err
		}
		versionID, err := r.appendVersion(ctx, tx, id, now)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE project SET series_id = ?, episode_number = 1, inherited_series_version_id = ? WHERE id = ?`, id, versionID, fromProjectID)
		return err
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		return domain.Series{}, err
	}
	if err != nil {
		return domain.Series{}, fmt.Errorf("create series: %w", err)
	}
	return r.Get(ctx, id)
}

// appendVersion appends the current canon as the next series_version and returns its ID. Versions
// are never updated or deleted.
func (r *SeriesRepository) appendVersion(ctx context.Context, tx *sql.Tx, seriesID, now string) (string, error) {
	canon, err := readCanon(ctx, tx, seriesID)
	if err != nil {
		return "", err
	}
	state, err := json.Marshal(canon)
	if err != nil {
		return "", err
	}
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM series_version WHERE series_id = ?`, seriesID).Scan(&next); err != nil {
		return "", err
	}
	id := domain.NewID()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO series_version (id, series_id, version_number, state, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, seriesID, next, string(state), now); err != nil {
		return "", fmt.Errorf("insert series version: %w", err)
	}
	return id, nil
}

// Get returns the Series or ErrNotFound.
func (r *SeriesRepository) Get(ctx context.Context, id string) (domain.Series, error) {
	var (
		s       domain.Series
		created string
	)
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT id, name, style, COALESCE(style_reference_asset_id, ''), created_at,
		        COALESCE((SELECT MAX(version_number) FROM series_version WHERE series_id = series.id), 0)
		 FROM series WHERE id = ?`, id).Scan(&s.ID, &s.Name, &s.Style, &s.StyleReferenceAssetID, &created, &s.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Series{}, ErrNotFound
	}
	if err != nil {
		return domain.Series{}, fmt.Errorf("get series: %w", err)
	}
	if s.CreatedAt, err = ParseTime(created); err != nil {
		return domain.Series{}, err
	}
	return s, nil
}

// Canon returns the current canon of the Series or ErrNotFound.
func (r *SeriesRepository) Canon(ctx context.Context, id string) (domain.SeriesCanon, error) {
	return readCanon(ctx, r.db.Reader(), id)
}

// CanonAt returns the canon as it was at the given version or ErrNotFound.
func (r *SeriesRepository) CanonAt(ctx context.Context, id string, version int) (domain.SeriesCanon, error) {
	var state string
	err := r.db.Reader().QueryRowContext(ctx, `SELECT state FROM series_version WHERE series_id = ? AND version_number = ?`, id, version).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SeriesCanon{}, ErrNotFound
	}
	if err != nil {
		return domain.SeriesCanon{}, fmt.Errorf("get series version: %w", err)
	}
	var c domain.SeriesCanon
	if err := json.Unmarshal([]byte(state), &c); err != nil {
		return domain.SeriesCanon{}, fmt.Errorf("decode series version %d: %w", version, err)
	}
	return c, nil
}

// Versions returns the history of the canon, newest first.
func (r *SeriesRepository) Versions(ctx context.Context, id string) ([]domain.SeriesVersion, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT version_number, created_at FROM series_version WHERE series_id = ? ORDER BY version_number DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("list series versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.SeriesVersion
	for rows.Next() {
		var (
			v       domain.SeriesVersion
			created string
		)
		if err := rows.Scan(&v.Number, &created); err != nil {
			return nil, err
		}
		if v.CreatedAt, err = ParseTime(created); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Episodes returns the Projects of the Series by episode number.
func (r *SeriesRepository) Episodes(ctx context.Context, id string) ([]domain.Project, error) {
	rows, err := r.db.Reader().QueryContext(ctx, projectSelect+` WHERE p.series_id = ? ORDER BY p.episode_number`, id)
	if err != nil {
		return nil, fmt.Errorf("list episodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan episode: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
