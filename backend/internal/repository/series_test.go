package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

type seriesFixture struct {
	db       *repository.DB
	series   *repository.SeriesRepository
	projects *repository.ProjectRepository
	world    *repository.WorldRepository
	// source is a Project with a Character (face image), a Product and a Location.
	source string
}

func newSeriesFixture(t *testing.T) seriesFixture {
	t.Helper()
	f := seriesFixture{db: openTestDB(t)}
	f.series = repository.NewSeriesRepository(f.db)
	f.projects = repository.NewProjectRepository(f.db)
	f.world = repository.NewWorldRepository(f.db)
	f.source = "proj-src"
	seedProject(t, f.db, f.source)
	for _, q := range []string{
		`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES ('face-1', 'proj-src', 'reference', 'proj-src/face-1.png', 'image/png', 10)`,
		`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES ('style-1', 'proj-src', 'reference', 'proj-src/style-1.png', 'image/png', 10)`,
		`INSERT INTO reference_item (id, project_id, asset_id, kind, name, source) VALUES ('ri-style', 'proj-src', 'style-1', 'style', 'watercolor', 'uploaded')`,
		`INSERT INTO character (id, project_id, name, hairstyle, wardrobe, emotion, position, face_reference_asset_id)
		 VALUES ('c-src', 'proj-src', 'Mio', 'bob', 'black_coat', 'angry', 'left', 'face-1')`,
		`INSERT INTO product (id, project_id, name, material, orientation, condition) VALUES ('p-src', 'proj-src', 'Serum', 'glass', 'label_front', 'unopened')`,
		`INSERT INTO location (id, project_id, name, time, weather) VALUES ('l-src', 'proj-src', 'Cafe', 'night', 'rain')`,
		`INSERT INTO brief (id, project_id, content) VALUES ('b-src', 'proj-src', '{"style":"soft watercolor"}')`,
	} {
		require.NoError(t, exec(t, f.db, q))
	}
	return f
}

func newEpisodeProject(id string) domain.Project {
	return domain.Project{ID: id, State: domain.StateDraft, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
		ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced}
}

func (f seriesFixture) createFromSource(t *testing.T) domain.Series {
	t.Helper()
	s, err := f.series.Create(context.Background(), "Mio Diaries", f.source)
	require.NoError(t, err)
	return s
}

func TestSeriesRepository_CreateFromProjectRegistersCanonAndEpisodeOne(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)
	s := f.createFromSource(t)

	assert.Equal(t, "Mio Diaries", s.Name)
	assert.Equal(t, "soft watercolor", s.Style)
	assert.Equal(t, "style-1", s.StyleReferenceAssetID)
	assert.Equal(t, 1, s.Version)

	canon, err := f.series.Canon(ctx, s.ID)
	require.NoError(t, err)
	require.Len(t, canon.Characters, 1)
	c := canon.Characters[0]
	assert.Equal(t, "Mio", c.Name)
	assert.Equal(t, "bob", c.Hairstyle)
	assert.Equal(t, "face-1", c.FaceReferenceAssetID)
	assert.Empty(t, c.Emotion, "the state a Shot leaves is not canon")
	assert.Empty(t, c.Position)
	require.Len(t, canon.Products, 1)
	assert.Equal(t, "unopened", canon.Products[0].Condition)
	assert.Empty(t, canon.Products[0].Orientation)
	require.Len(t, canon.Locations, 1)
	assert.Empty(t, canon.Locations[0].Time)
	assert.Empty(t, canon.Locations[0].Weather)

	// The source Project is Episode 1 and its rows point at the canon rows.
	p, err := f.projects.Get(ctx, f.source)
	require.NoError(t, err)
	assert.Equal(t, s.ID, p.SeriesID)
	assert.Equal(t, 1, p.EpisodeNumber)
	w, err := f.world.World(ctx, f.source, "")
	require.NoError(t, err)
	assert.Equal(t, "face-1", w.Characters[0].SeriesFaceReferenceAssetID)
	assert.Equal(t, "angry", w.Characters[0].Emotion, "the source Project keeps its own state")
}

func TestSeriesRepository_CreateWithoutProjectIsEmptyAndRejectsBadSources(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)

	empty, err := f.series.Create(ctx, "Empty", "")
	require.NoError(t, err)
	canon, err := f.series.Canon(ctx, empty.ID)
	require.NoError(t, err)
	assert.Empty(t, canon.Characters)
	assert.Equal(t, 1, empty.Version)

	_, err = f.series.Create(ctx, "Ghost", "no-such-project")
	assert.ErrorIs(t, err, repository.ErrNotFound)

	f.createFromSource(t)
	_, err = f.series.Create(ctx, "Again", f.source)
	assert.ErrorIs(t, err, repository.ErrConflict, "a Project belongs to one Series")
}

func TestSeriesRepository_CreateEpisodeCopiesCanonWithOrigin(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)
	s := f.createFromSource(t)

	n, err := f.series.CreateEpisode(ctx, s.ID, "", "", newEpisodeProject("ep2"))
	require.NoError(t, err)
	assert.Equal(t, 2, n, "the source Project is Episode 1")

	p, err := f.projects.Get(ctx, "ep2")
	require.NoError(t, err)
	assert.Equal(t, s.ID, p.SeriesID)
	assert.Equal(t, 2, p.EpisodeNumber)
	assert.Equal(t, "Mio Diaries 第2話", p.Name)

	canon, err := f.series.Canon(ctx, s.ID)
	require.NoError(t, err)
	w, err := f.world.World(ctx, "ep2", "")
	require.NoError(t, err)
	require.Len(t, w.Characters, 1)
	ch := w.Characters[0]
	assert.NotEqual(t, canon.Characters[0].ID, ch.ID, "the Episode has its own row")
	assert.Equal(t, "bob", ch.Hairstyle)
	assert.Empty(t, ch.Emotion, "an Episode starts without the state of the source")
	assert.Equal(t, "face-1", ch.SeriesFaceReferenceAssetID, "the face is measured against the canon face")
	require.Len(t, w.Products, 1)
	assert.Equal(t, "unopened", w.Products[0].Condition)
	require.Len(t, w.Locations, 1)
	assert.Equal(t, "Cafe", w.Locations[0].Name)

	var origin string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT origin_id FROM character WHERE id = ?`, ch.ID).Scan(&origin))
	assert.Equal(t, canon.Characters[0].ID, origin)

	// The image is the Project's own asset row (the file is shared) and is in its Library.
	var assetProject, assetPath string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT project_id, file_path FROM asset WHERE id = ?`, ch.FaceReferenceAssetID).Scan(&assetProject, &assetPath))
	assert.Equal(t, "ep2", assetProject)
	assert.Equal(t, "proj-src/face-1.png", assetPath)
	var libraryName string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT name FROM reference_item WHERE project_id = 'ep2' AND asset_id = ?`, ch.FaceReferenceAssetID).Scan(&libraryName))
	assert.Equal(t, "Mio", libraryName)
	var styleItems int
	require.NoError(t, f.db.Reader().QueryRow(`SELECT COUNT(*) FROM reference_item WHERE project_id = 'ep2' AND kind = 'style'`).Scan(&styleItems))
	assert.Equal(t, 1, styleItems, "the Style image is inherited too")

	brief, err := f.projects.GetBrief(ctx, "ep2")
	require.NoError(t, err)
	assert.Equal(t, "soft watercolor", brief.Style)

	var seriesVersion, worldVersion any
	require.NoError(t, f.db.Reader().QueryRow(`SELECT inherited_series_version_id, inherited_world_state_version_id FROM project WHERE id = 'ep2'`).Scan(&seriesVersion, &worldVersion))
	assert.NotNil(t, seriesVersion, "the canon version the Episode inherited is recorded")
	assert.Nil(t, worldVersion, "no previous Episode was named")

	// Episode numbers are unique per Series.
	err = exec(t, f.db, `INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy, series_id, episode_number)
		VALUES ('dup', 'auto', 'key_check', 'product_cm', 'balanced', ?, 2)`, s.ID)
	assert.Error(t, err)

	n, err = f.series.CreateEpisode(ctx, s.ID, "", "", newEpisodeProject("ep3"))
	require.NoError(t, err)
	assert.Equal(t, 3, n)
}

func TestSeriesRepository_CreateEpisodeStartsFromPreviousWorldState(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)
	s := f.createFromSource(t)

	// The source Project (Episode 1) ends with a World State: an angry Mio in the rain at the cafe.
	require.NoError(t, exec(t, f.db, `UPDATE location SET lighting_state = 'neon' WHERE id = 'l-src'`))
	require.NoError(t, exec(t, f.db, `INSERT INTO world_state_version (id, project_id, version_number, state) VALUES ('wsv-1', 'proj-src', 1, ?)`,
		`{"characters":[],"products":[],"locations":[],"camera":{"lens":"35mm"},"story":{"scene":4,"character_goal":"find the letter","previous_event":"the door slammed"}}`))

	_, err := f.series.CreateEpisode(ctx, s.ID, f.source, "", newEpisodeProject("ep2"))
	require.NoError(t, err)

	w, err := f.world.World(ctx, "ep2", "")
	require.NoError(t, err)
	assert.Equal(t, "angry", w.Characters[0].Emotion)
	assert.Equal(t, "left", w.Characters[0].Position)
	assert.Equal(t, "label_front", w.Products[0].Orientation)
	assert.Equal(t, "night", w.Locations[0].Time)
	assert.Equal(t, "neon", w.Locations[0].LightingState)
	assert.Equal(t, "35mm", w.Camera.Lens)
	assert.Equal(t, "find the letter", w.Story.CharacterGoal)
	assert.Equal(t, "the door slammed", w.Story.PreviousEvent)
	assert.Zero(t, w.Story.Scene, "the Scene number counts within an Episode")
	assert.Equal(t, 1, w.Version, "the inherited state is the first version of the Episode")

	var inherited string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT inherited_world_state_version_id FROM project WHERE id = 'ep2'`).Scan(&inherited))
	assert.Equal(t, "wsv-1", inherited)
}

func TestSeriesRepository_CreateEpisodeInheritsTheTrunkWorldStateNotABranchs(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)
	s := f.createFromSource(t)

	// The previous Episode has a trunk version 1 and a Branch whose own numbering reaches 3.
	require.NoError(t, exec(t, f.db, `INSERT INTO scene (id, project_id, order_index) VALUES ('sc-src', 'proj-src', 0)`))
	require.NoError(t, exec(t, f.db, `INSERT INTO shot (id, scene_id, order_index) VALUES ('sh-src', 'sc-src', 0)`))
	require.NoError(t, exec(t, f.db, `INSERT INTO branch (id, project_id, branch_point_type, branch_point_shot_id, name) VALUES ('br-src', 'proj-src', 'shot', 'sh-src', 'alt')`))
	require.NoError(t, exec(t, f.db, `INSERT INTO world_state_version (id, project_id, branch_id, version_number, state) VALUES ('wsv-trunk', 'proj-src', NULL, 1, ?)`,
		`{"characters":[],"products":[],"locations":[],"camera":{"lens":"35mm"},"story":{"scene":4,"character_goal":"find the letter"}}`))
	for i, id := range []string{"wsv-br-1", "wsv-br-2", "wsv-br-3"} {
		require.NoError(t, exec(t, f.db, `INSERT INTO world_state_version (id, project_id, branch_id, version_number, state) VALUES (?, 'proj-src', 'br-src', ?, ?)`,
			id, i+1, `{"characters":[],"products":[],"locations":[],"camera":{"lens":"85mm"},"story":{"scene":9,"character_goal":"leave town"}}`))
	}

	_, err := f.series.CreateEpisode(ctx, s.ID, f.source, "", newEpisodeProject("ep2"))
	require.NoError(t, err)

	w, err := f.world.World(ctx, "ep2", "")
	require.NoError(t, err)
	assert.Equal(t, "35mm", w.Camera.Lens, "the Branch's Camera is not inherited")
	assert.Equal(t, "find the letter", w.Story.CharacterGoal, "the Branch's Story is not inherited")
	var inherited string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT inherited_world_state_version_id FROM project WHERE id = 'ep2'`).Scan(&inherited))
	assert.Equal(t, "wsv-trunk", inherited)
}

func TestSeriesRepository_CreateEpisodeRejectsUnknownSeriesAndForeignPrevious(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)
	s := f.createFromSource(t)

	_, err := f.series.CreateEpisode(ctx, "no-such-series", "", "", newEpisodeProject("ep2"))
	assert.ErrorIs(t, err, repository.ErrNotFound)

	seedProject(t, f.db, "loner")
	_, err = f.series.CreateEpisode(ctx, s.ID, "loner", "", newEpisodeProject("ep2"))
	assert.ErrorIs(t, err, repository.ErrNotInSeries)
	_, err = f.projects.Get(ctx, "ep2")
	assert.ErrorIs(t, err, repository.ErrNotFound, "a rejected Episode leaves nothing behind")
}

func TestSeriesRepository_CanonUpdateIsVersionedAndDoesNotReachEpisodes(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)
	s := f.createFromSource(t)
	_, err := f.series.CreateEpisode(ctx, s.ID, "", "", newEpisodeProject("ep2"))
	require.NoError(t, err)
	canon, err := f.series.Canon(ctx, s.ID)
	require.NoError(t, err)
	canonID := canon.Characters[0].ID

	require.NoError(t, f.series.UpdateCharacter(ctx, s.ID, canonID, map[string]string{"hairstyle": "long", "wardrobe": ""}))
	require.NoError(t, f.series.UpdateStyle(ctx, s.ID, "flat anime"))

	updated, err := f.series.Get(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, updated.Version, "each change appends a version")
	assert.Equal(t, "flat anime", updated.Style)
	now, err := f.series.Canon(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, "long", now.Characters[0].Hairstyle)
	assert.Empty(t, now.Characters[0].Wardrobe)

	// History: version 1 is what was registered, version 2 has the hairstyle, version 3 the style.
	v1, err := f.series.CanonAt(ctx, s.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "bob", v1.Characters[0].Hairstyle)
	assert.Equal(t, "soft watercolor", v1.Style)
	v2, err := f.series.CanonAt(ctx, s.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, "long", v2.Characters[0].Hairstyle)
	assert.Equal(t, "soft watercolor", v2.Style)
	versions, err := f.series.Versions(ctx, s.ID)
	require.NoError(t, err)
	require.Len(t, versions, 3)
	assert.Equal(t, 3, versions[0].Number, "newest first")
	_, err = f.series.CanonAt(ctx, s.ID, 9)
	assert.ErrorIs(t, err, repository.ErrNotFound)

	// The existing Episodes are untouched.
	for _, id := range []string{"ep2", f.source} {
		w, err := f.world.World(ctx, id, "")
		require.NoError(t, err)
		assert.Equal(t, "bob", w.Characters[0].Hairstyle, id)
		assert.Equal(t, "black_coat", w.Characters[0].Wardrobe, id)
	}
	brief, err := f.projects.GetBrief(ctx, "ep2")
	require.NoError(t, err)
	assert.Equal(t, "soft watercolor", brief.Style)

	// A new Episode inherits the updated canon.
	_, err = f.series.CreateEpisode(ctx, s.ID, "", "", newEpisodeProject("ep3"))
	require.NoError(t, err)
	w, err := f.world.World(ctx, "ep3", "")
	require.NoError(t, err)
	assert.Equal(t, "long", w.Characters[0].Hairstyle)
}

func TestSeriesRepository_UpdateCharacterRejectsForeignAndNonCanonFields(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)
	s := f.createFromSource(t)

	assert.ErrorIs(t, f.series.UpdateCharacter(ctx, s.ID, "c-src", map[string]string{"hairstyle": "x"}), repository.ErrNotFound,
		"a Character of an Episode is not canon")
	assert.ErrorIs(t, f.series.UpdateCharacter(ctx, "no-such-series", "c-src", map[string]string{"hairstyle": "x"}), repository.ErrNotFound)
	assert.ErrorIs(t, f.series.UpdateStyle(ctx, "no-such-series", "x"), repository.ErrNotFound)

	canon, err := f.series.Canon(ctx, s.ID)
	require.NoError(t, err)
	assert.Error(t, f.series.UpdateCharacter(ctx, s.ID, canon.Characters[0].ID, map[string]string{"emotion": "x"}))
	got, err := f.series.Get(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Version, "a rejected update appends no version")
}

func TestSeriesRepository_EpisodesListsByNumber(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)
	s := f.createFromSource(t)
	_, err := f.series.CreateEpisode(ctx, s.ID, "", "", newEpisodeProject("ep2"))
	require.NoError(t, err)

	eps, err := f.series.Episodes(ctx, s.ID)
	require.NoError(t, err)
	require.Len(t, eps, 2)
	assert.Equal(t, f.source, eps[0].ID)
	assert.Equal(t, "ep2", eps[1].ID)
}

func TestSeriesRepository_ABranchOfAnEpisodeKeepsTheCanonOfItsCharacters(t *testing.T) {
	ctx := context.Background()
	f := newSeriesFixture(t)
	s := f.createFromSource(t)
	_, err := f.series.CreateEpisode(ctx, s.ID, "", "", newEpisodeProject("ep2"))
	require.NoError(t, err)

	plans := repository.NewPlanRepository(f.db)
	scene, err := plans.CreateScene(ctx, domain.Scene{ProjectID: "ep2", Title: "A", Location: "Cafe"})
	require.NoError(t, err)
	shot, err := plans.CreateShot(ctx, domain.Shot{SceneID: scene.ID, Objective: "one", Duration: 2})
	require.NoError(t, err)

	b, err := repository.NewBranchRepository(f.db).Create(ctx, repository.NewBranch{ShotID: shot.ID, PointType: domain.BranchAtShot, Name: "Alt"})
	require.NoError(t, err)

	w, err := f.world.World(ctx, "ep2", b.ID)
	require.NoError(t, err)
	require.Len(t, w.Characters, 1)
	assert.Equal(t, "face-1", w.Characters[0].SeriesFaceReferenceAssetID, "the Branch is measured against the canon face like the trunk")
	for table, n := range map[string]int{"character": 1, "product": 1, "location": 1} {
		var withOrigin int
		require.NoError(t, f.db.Reader().QueryRow(
			`SELECT COUNT(*) FROM `+table+` WHERE project_id = 'ep2' AND branch_id = ? AND origin_id IS NOT NULL`, b.ID).Scan(&withOrigin))
		assert.Equal(t, n, withOrigin, table)
	}
}
