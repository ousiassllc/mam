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
			`INSERT INTO character (id, series_id, name, hairstyle, wardrobe, voice, personality, face_reference_asset_id, body_reference_asset_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			canonID, seriesID, c.Name, nullIfEmpty(c.Hairstyle), nullIfEmpty(c.Wardrobe), nullIfEmpty(c.Voice), nullIfEmpty(c.Personality),
			nullIfEmpty(c.FaceReferenceAssetID), nullIfEmpty(c.BodyReferenceAssetID)); err != nil {
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
	chars, err := q.QueryContext(ctx,
		`SELECT id, name, COALESCE(hairstyle, ''), COALESCE(wardrobe, ''), COALESCE(voice, ''), COALESCE(personality, ''),
		        COALESCE(face_reference_asset_id, ''), COALESCE(body_reference_asset_id, '')
		 FROM character WHERE series_id = ? ORDER BY name, id`, seriesID)
	if err != nil {
		return c, fmt.Errorf("list canon characters: %w", err)
	}
	defer func() { _ = chars.Close() }()
	for chars.Next() {
		var ch domain.WorldCharacter
		if err := chars.Scan(&ch.ID, &ch.Name, &ch.Hairstyle, &ch.Wardrobe, &ch.Voice, &ch.Personality, &ch.FaceReferenceAssetID, &ch.BodyReferenceAssetID); err != nil {
			return c, err
		}
		c.Characters = append(c.Characters, ch)
	}
	if err := chars.Err(); err != nil {
		return c, err
	}
	prods, err := q.QueryContext(ctx,
		`SELECT id, name, COALESCE(material, ''), COALESCE(condition, ''),
		        COALESCE(geometry_reference_asset_id, ''), COALESCE(label_reference_asset_id, '')
		 FROM product WHERE series_id = ? ORDER BY name, id`, seriesID)
	if err != nil {
		return c, fmt.Errorf("list canon products: %w", err)
	}
	defer func() { _ = prods.Close() }()
	for prods.Next() {
		var p domain.WorldProduct
		if err := prods.Scan(&p.ID, &p.Name, &p.Material, &p.Condition, &p.GeometryReferenceAssetID, &p.LabelReferenceAssetID); err != nil {
			return c, err
		}
		c.Products = append(c.Products, p)
	}
	if err := prods.Err(); err != nil {
		return c, err
	}
	locs, err := q.QueryContext(ctx,
		`SELECT id, name, COALESCE(layout_reference_asset_id, '') FROM location WHERE series_id = ? ORDER BY name, id`, seriesID)
	if err != nil {
		return c, fmt.Errorf("list canon locations: %w", err)
	}
	defer func() { _ = locs.Close() }()
	for locs.Next() {
		var l domain.WorldLocation
		if err := locs.Scan(&l.ID, &l.Name, &l.LayoutReferenceAssetID); err != nil {
			return c, err
		}
		c.Locations = append(c.Locations, l)
	}
	return c, locs.Err()
}

// UpdateCharacter sets fields of a canonical Character (domain.SeriesCharacterFields) and appends
// the canon as a new version. Episodes that already copied the Character keep their values. An
// unknown Series or Character is ErrNotFound.
func (r *SeriesRepository) UpdateCharacter(ctx context.Context, seriesID, characterID string, fields map[string]string) error {
	for name := range fields {
		if !domain.IsSeriesCharacterField(name) {
			return fmt.Errorf("update series character: field %q is not editable", name)
		}
	}
	now := formatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
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
		_, err = r.appendVersion(ctx, tx, seriesID, now)
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("update series character: %w", err)
	}
	return nil
}

// UpdateStyle sets the canonical Style text and appends the canon as a new version. Episodes keep
// the Style they inherited. An unknown Series is ErrNotFound.
func (r *SeriesRepository) UpdateStyle(ctx context.Context, seriesID, style string) error {
	now := formatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE series SET style = ? WHERE id = ?`, style, seriesID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return ErrNotFound
		}
		_, err = r.appendVersion(ctx, tx, seriesID, now)
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("update series style: %w", err)
	}
	return nil
}
