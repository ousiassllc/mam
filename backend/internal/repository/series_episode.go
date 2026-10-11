package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// CreateEpisode adds the Project p as the next Episode of the Series and returns its number. The
// current canon is copied into the Project: Character, Product and Location rows with origin_id
// pointing at the canon row, their reference images as Assets and Reference Library entries of the
// Project, and the Style into its Brief. inherited_series_version_id records the canon version.
// With previousEpisodeID the Project starts from the final World State of that Episode: the state
// of the rows with the same origin, the Camera and the Story, as its first world_state_version
// (inherited_world_state_version_id is that Episode's latest version). An unknown Series is
// ErrNotFound; a previous Project that is not an Episode of the Series is ErrNotInSeries. With
// brandTemplateID the Brand Template's constraints are copied into the Project (an unknown template
// is ErrNotFound and nothing is created).
func (r *SeriesRepository) CreateEpisode(ctx context.Context, seriesID, previousEpisodeID, brandTemplateID string, p domain.Project) (int, error) {
	var number int
	now := FormatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		canon, err := readCanon(ctx, tx, seriesID)
		if err != nil {
			return err
		}
		if previousEpisodeID != "" {
			var one int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM project WHERE id = ? AND series_id = ?`, previousEpisodeID, seriesID).Scan(&one)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotInSeries
			}
			if err != nil {
				return err
			}
		}
		var versionID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM series_version WHERE series_id = ? ORDER BY version_number DESC LIMIT 1`, seriesID).Scan(&versionID); err != nil {
			return fmt.Errorf("latest series version: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(episode_number), 0) + 1 FROM project WHERE series_id = ?`, seriesID).Scan(&number); err != nil {
			return err
		}
		if err := insertProject(ctx, tx, p, domain.Brief{Style: canon.Style}, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE project SET series_id = ?, episode_number = ?, inherited_series_version_id = ? WHERE id = ?`,
			seriesID, number, versionID, p.ID); err != nil {
			return err
		}
		if err := copyCanon(ctx, tx, canon, p.ID, now); err != nil {
			return err
		}
		if err := copyNotes(ctx, tx, seriesID, p.ID, now); err != nil {
			return err
		}
		if brandTemplateID != "" {
			if err := applyBrandTemplate(ctx, tx, brandTemplateID, p.ID, now); err != nil {
				return err
			}
		}
		if previousEpisodeID == "" {
			return nil
		}
		return r.inheritWorldState(ctx, tx, previousEpisodeID, p.ID)
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotInSeries) {
		return 0, err
	}
	if err != nil {
		return 0, fmt.Errorf("create episode: %w", err)
	}
	return number, nil
}

// assetCopier gives a Project its own asset rows for images that belong to another Project. The
// file is shared: it is never rewritten or removed by anything but the Take archive of its own
// Project, and the canon images are not Takes.
type assetCopier struct {
	tx        *sql.Tx
	projectID string
	now       string
	copies    map[string]string
}

// copy returns the ID of the Project's copy of the asset and whether the copy is new. It returns
// "" when the asset is "" or gone.
func (c *assetCopier) copy(ctx context.Context, assetID string) (string, bool, error) {
	if assetID == "" {
		return "", false, nil
	}
	if id, ok := c.copies[assetID]; ok {
		return id, false, nil
	}
	a := domain.Asset{ID: domain.NewID(), ProjectID: c.projectID}
	err := c.tx.QueryRowContext(ctx, `SELECT type, file_path, COALESCE(mime_type, ''), COALESCE(size_bytes, 0) FROM asset WHERE id = ?`, assetID).
		Scan(&a.Type, &a.FilePath, &a.MimeType, &a.SizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if err := insertAsset(ctx, c.tx, a, c.now); err != nil {
		return "", false, err
	}
	c.copies[assetID] = a.ID
	return a.ID, true, nil
}

// reference copies the asset and registers the copy in the Library of the Project (once per
// asset: an image is one Library entry, whichever slots hold it). It returns the asset ID to store.
func (c *assetCopier) reference(ctx context.Context, assetID string, kind domain.ReferenceKind, name string) (string, error) {
	copied, isNew, err := c.copy(ctx, assetID)
	if err != nil || copied == "" {
		return "", err
	}
	if !isNew {
		return copied, nil
	}
	err = insertReferenceItem(ctx, c.tx, domain.ReferenceItem{
		ID: domain.NewID(), ProjectID: c.projectID, AssetID: copied, Kind: kind, Name: name, Source: domain.SourceUploaded,
	}, c.now)
	return copied, err
}

// bodyNameSuffix and labelNameSuffix keep the Library names of a Character's body image and a
// Product's label image apart from the face and shape images, which carry the entity's own name
// (the Reference Pack finds a Character's face by that name).
const (
	bodyNameSuffix  = "（全身）"
	labelNameSuffix = "（ラベル）"
	styleItemName   = "Series Style"
)

// copyCanon writes the canon into the Project as rows with origin_id, copied images and Library entries.
func copyCanon(ctx context.Context, tx *sql.Tx, canon domain.SeriesCanon, projectID, now string) error {
	ac := &assetCopier{tx: tx, projectID: projectID, now: now, copies: map[string]string{}}
	for _, c := range canon.Characters {
		face, err := ac.reference(ctx, c.FaceReferenceAssetID, domain.RefCharacter, c.Name)
		if err != nil {
			return err
		}
		body, err := ac.reference(ctx, c.BodyReferenceAssetID, domain.RefCharacter, c.Name+bodyNameSuffix)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO character (id, project_id, origin_id, name, hairstyle, wardrobe, voice, personality, face_reference_asset_id, body_reference_asset_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			domain.NewID(), projectID, c.ID, c.Name, nullIfEmpty(c.Hairstyle), nullIfEmpty(c.Wardrobe), nullIfEmpty(c.Voice), nullIfEmpty(c.Personality),
			nullIfEmpty(face), nullIfEmpty(body)); err != nil {
			return fmt.Errorf("copy character: %w", err)
		}
	}
	for _, p := range canon.Products {
		geometry, err := ac.reference(ctx, p.GeometryReferenceAssetID, domain.RefProduct, p.Name)
		if err != nil {
			return err
		}
		label, err := ac.reference(ctx, p.LabelReferenceAssetID, domain.RefProduct, p.Name+labelNameSuffix)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO product (id, project_id, origin_id, name, material, condition, geometry_reference_asset_id, label_reference_asset_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			domain.NewID(), projectID, p.ID, p.Name, nullIfEmpty(p.Material), nullIfEmpty(p.Condition), nullIfEmpty(geometry), nullIfEmpty(label)); err != nil {
			return fmt.Errorf("copy product: %w", err)
		}
	}
	for _, l := range canon.Locations {
		layout, err := ac.reference(ctx, l.LayoutReferenceAssetID, domain.RefLocation, l.Name)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO location (id, project_id, origin_id, name, layout_reference_asset_id) VALUES (?, ?, ?, ?, ?)`,
			domain.NewID(), projectID, l.ID, l.Name, nullIfEmpty(layout)); err != nil {
			return fmt.Errorf("copy location: %w", err)
		}
	}
	_, err := ac.reference(ctx, canon.StyleReferenceAssetID, domain.RefStyle, styleItemName)
	return err
}

// stateUpdates copy, from the previous Episode's row with the same origin, the columns of each
// entity table that a Shot changes (the complement of the canon fields), from the trunk row only: a
// Branch holds copies with the same origin. The parameters are the
// new Project and the previous Episode.
var stateUpdates = []string{
	`UPDATE character SET emotion = prev.emotion, position = prev.position, current_action = prev.current_action
	 FROM character AS prev WHERE character.project_id = ? AND prev.project_id = ? AND prev.branch_id IS NULL AND prev.origin_id = character.origin_id`,
	`UPDATE product SET orientation = prev.orientation
	 FROM product AS prev WHERE product.project_id = ? AND prev.project_id = ? AND prev.branch_id IS NULL AND prev.origin_id = product.origin_id`,
	`UPDATE location SET time = prev.time, lighting_state = prev.lighting_state, weather = prev.weather
	 FROM location AS prev WHERE location.project_id = ? AND prev.project_id = ? AND prev.branch_id IS NULL AND prev.origin_id = location.origin_id`,
}

// inheritWorldState starts the Project from the final World State of the previous Episode: its trunk
// (branch_id IS NULL), never a Branch, whose version numbers are counted separately.
func (r *SeriesRepository) inheritWorldState(ctx context.Context, tx *sql.Tx, previousID, projectID string) error {
	for _, q := range stateUpdates {
		if _, err := tx.ExecContext(ctx, q, projectID, previousID); err != nil {
			return fmt.Errorf("inherit world state rows: %w", err)
		}
	}
	var (
		prevVersionID, state string
	)
	err := tx.QueryRowContext(ctx,
		`SELECT id, state FROM world_state_version WHERE project_id = ? AND branch_id IS NULL ORDER BY version_number DESC LIMIT 1`, previousID).Scan(&prevVersionID, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // the previous Episode has completed no Shot: no Camera or Story to inherit
	}
	if err != nil {
		return err
	}
	var snapshot domain.WorldContext
	if err := json.Unmarshal([]byte(state), &snapshot); err != nil {
		return fmt.Errorf("decode world state of the previous episode: %w", err)
	}
	w, err := readWorld(ctx, tx, projectID, "")
	if err != nil {
		return err
	}
	w.Camera, w.Story = snapshot.Camera, snapshot.Story
	w.Story.Scene = 0 // the Scene number counts within an Episode
	if _, _, err := r.world.appendVersion(ctx, tx, projectID, "", w); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE project SET inherited_world_state_version_id = ? WHERE id = ?`, prevVersionID, projectID)
	return err
}

// copyNotes gives the Project (an Episode of the Series) its own memos: the latest version of every
// active canonical note of the Series becomes version 1 of a Project note whose origin_id is the
// canonical note. Archived canonical notes are not copied, and later changes of the canon do not
// reach the copies.
func copyNotes(ctx context.Context, tx *sql.Tx, seriesID, projectID, now string) error {
	rows, err := tx.QueryContext(ctx, noteSelect+` WHERE n.series_id = ? AND n.archived_at IS NULL ORDER BY n.created_at, n.id`, seriesID)
	if err != nil {
		return fmt.Errorf("list canonical notes: %w", err)
	}
	var canon []domain.NoteView
	for rows.Next() {
		v, err := scanNoteView(rows)
		if err != nil {
			_ = rows.Close()
			return err
		}
		canon = append(canon, v)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, c := range canon {
		id := domain.NewID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO note (id, project_id, origin_id, category, pinned, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, projectID, c.ID, c.Category, boolInt(c.Pinned), now); err != nil {
			return fmt.Errorf("copy note: %w", err)
		}
		if err := insertNoteVersion(ctx, tx, id, 1, c.Version.Title, c.Version.Content, c.Version.Source, now); err != nil {
			return err
		}
	}
	return nil
}
