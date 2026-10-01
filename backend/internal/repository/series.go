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
	now := formatTime(r.now())
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
	if s.CreatedAt, err = parseTime(created); err != nil {
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
		if v.CreatedAt, err = parseTime(created); err != nil {
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
	now := formatTime(r.now())
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
// entity table that a Shot changes (the complement of the canon fields). The parameters are the
// new Project and the previous Episode.
var stateUpdates = []string{
	`UPDATE character SET emotion = prev.emotion, position = prev.position, current_action = prev.current_action
	 FROM character AS prev WHERE character.project_id = ? AND prev.project_id = ? AND prev.origin_id = character.origin_id`,
	`UPDATE product SET orientation = prev.orientation
	 FROM product AS prev WHERE product.project_id = ? AND prev.project_id = ? AND prev.origin_id = product.origin_id`,
	`UPDATE location SET time = prev.time, lighting_state = prev.lighting_state, weather = prev.weather
	 FROM location AS prev WHERE location.project_id = ? AND prev.project_id = ? AND prev.origin_id = location.origin_id`,
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
