package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// registerCanon copies the canonical fields of the Project's rows into canon rows of the Series
// and points the Project's rows at them (origin_id).
func registerCanon(ctx context.Context, tx *sql.Tx, seriesID, projectID string) error {
	w, err := readWorld(ctx, tx, projectID, "")
	if err != nil {
		return err
	}
	for _, c := range w.Characters {
		c = domain.CanonCharacter(c)
		canonID := domain.NewID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO character (id, series_id, name, hairstyle, wardrobe, voice, personality, face_reference_asset_id, body_reference_asset_id, voice_reference)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			canonID, seriesID, c.Name, nullIfEmpty(c.Hairstyle), nullIfEmpty(c.Wardrobe), nullIfEmpty(c.Voice), nullIfEmpty(c.Personality),
			nullIfEmpty(c.FaceReferenceAssetID), nullIfEmpty(c.BodyReferenceAssetID), c.VoiceReference); err != nil {
			return fmt.Errorf("insert canon character: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE character SET origin_id = ? WHERE id = ?`, canonID, c.ID); err != nil {
			return err
		}
	}
	for _, p := range w.Products {
		p = domain.CanonProduct(p)
		canonID := domain.NewID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO product (id, series_id, name, material, condition, geometry_reference_asset_id, label_reference_asset_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			canonID, seriesID, p.Name, nullIfEmpty(p.Material), nullIfEmpty(p.Condition),
			nullIfEmpty(p.GeometryReferenceAssetID), nullIfEmpty(p.LabelReferenceAssetID)); err != nil {
			return fmt.Errorf("insert canon product: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE product SET origin_id = ? WHERE id = ?`, canonID, p.ID); err != nil {
			return err
		}
	}
	for _, l := range w.Locations {
		l = domain.CanonLocation(l)
		canonID := domain.NewID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO location (id, series_id, name, layout_reference_asset_id) VALUES (?, ?, ?, ?)`,
			canonID, seriesID, l.Name, nullIfEmpty(l.LayoutReferenceAssetID)); err != nil {
			return fmt.Errorf("insert canon location: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE location SET origin_id = ? WHERE id = ?`, canonID, l.ID); err != nil {
			return err
		}
	}
	return nil
}

// readCanon reads the current canon of the Series: ErrNotFound when the Series does not exist.
func readCanon(ctx context.Context, q queryer, seriesID string) (domain.SeriesCanon, error) {
	var c domain.SeriesCanon
	err := q.QueryRowContext(ctx, `SELECT name, style, COALESCE(style_reference_asset_id, '') FROM series WHERE id = ?`, seriesID).
		Scan(&c.Name, &c.Style, &c.StyleReferenceAssetID)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, fmt.Errorf("get series: %w", err)
	}
	c.Characters, err = queryAll(ctx, q, func(row rowScanner) (domain.WorldCharacter, error) {
		var ch domain.WorldCharacter
		return ch, row.Scan(&ch.ID, &ch.Name, &ch.Hairstyle, &ch.Wardrobe, &ch.Voice, &ch.Personality, &ch.FaceReferenceAssetID, &ch.BodyReferenceAssetID, &ch.VoiceReference)
	}, `SELECT id, name, COALESCE(hairstyle, ''), COALESCE(wardrobe, ''), COALESCE(voice, ''), COALESCE(personality, ''),
		        COALESCE(face_reference_asset_id, ''), COALESCE(body_reference_asset_id, ''), voice_reference
		 FROM character WHERE series_id = ? ORDER BY name, id`, seriesID)
	if err != nil {
		return c, fmt.Errorf("list canon characters: %w", err)
	}
	c.Products, err = queryAll(ctx, q, func(row rowScanner) (domain.WorldProduct, error) {
		var p domain.WorldProduct
		return p, row.Scan(&p.ID, &p.Name, &p.Material, &p.Condition, &p.GeometryReferenceAssetID, &p.LabelReferenceAssetID)
	}, `SELECT id, name, COALESCE(material, ''), COALESCE(condition, ''),
		        COALESCE(geometry_reference_asset_id, ''), COALESCE(label_reference_asset_id, '')
		 FROM product WHERE series_id = ? ORDER BY name, id`, seriesID)
	if err != nil {
		return c, fmt.Errorf("list canon products: %w", err)
	}
	c.Locations, err = queryAll(ctx, q, func(row rowScanner) (domain.WorldLocation, error) {
		var l domain.WorldLocation
		return l, row.Scan(&l.ID, &l.Name, &l.LayoutReferenceAssetID)
	}, `SELECT id, name, COALESCE(layout_reference_asset_id, '') FROM location WHERE series_id = ? ORDER BY name, id`, seriesID)
	if err != nil {
		return c, fmt.Errorf("list canon locations: %w", err)
	}
	return c, nil
}

// EditCanon runs edit in a write transaction and appends the canon it leaves as a new version (Episodes keep their
// copies). edit returning ErrNotFound (an unknown Series or Character) writes nothing.
func (r *SeriesRepository) EditCanon(ctx context.Context, seriesID string, edit func(ctx context.Context, tx *sql.Tx) error) error {
	now := FormatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := edit(ctx, tx); err != nil {
			return err
		}
		_, err := r.appendVersion(ctx, tx, seriesID, now)
		return err
	})
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("edit series canon: %w", err)
	}
	return err
}

// UpdateCharacter sets fields of a canonical Character (domain.SeriesCharacterFields) and appends the canon as a
// new version; Episodes keep their copies. An unknown Series or Character is ErrNotFound.
func (r *SeriesRepository) UpdateCharacter(ctx context.Context, seriesID, characterID string, fields map[string]string) error {
	for name := range fields {
		if !domain.IsSeriesCharacterField(name) {
			return fmt.Errorf("update series character: field %q is not editable", name)
		}
	}
	return r.EditCanon(ctx, seriesID, func(ctx context.Context, tx *sql.Tx) error {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM character WHERE id = ? AND series_id = ?`, characterID, seriesID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		for name, value := range fields {
			if err := setField(ctx, tx, "character", characterID, name, value); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateStyle sets the canonical Style text and appends the canon as a new version. Episodes keep
// the Style they inherited. An unknown Series is ErrNotFound.
func (r *SeriesRepository) UpdateStyle(ctx context.Context, seriesID, style string) error {
	return r.EditCanon(ctx, seriesID, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE series SET style = ? WHERE id = ?`, style, seriesID)
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
}
