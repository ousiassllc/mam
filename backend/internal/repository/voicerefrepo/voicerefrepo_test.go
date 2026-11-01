package voicerefrepo_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/repository/voicerefrepo"
)

type fixture struct {
	db     *repository.DB
	series *repository.SeriesRepository
	world  *repository.WorldRepository
	repo   *voicerefrepo.Repository
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := testdb.Open(t)
	series := repository.NewSeriesRepository(db)
	f := fixture{db: db, series: series, world: repository.NewWorldRepository(db), repo: voicerefrepo.New(db, series)}
	f.exec(t, `INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('src', 'auto', 'key_check', 'product_cm', 'balanced')`)
	f.exec(t, `INSERT INTO character (id, project_id, name, hairstyle) VALUES ('c-src', 'src', 'Mio', 'bob')`)
	return f
}

func (f fixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

func ref(path, text string) *voiceref.Reference {
	return &voiceref.Reference{FilePath: path, MimeType: "audio/wav", SizeBytes: 100, DurationMS: 5000, Text: text, Language: "ja"}
}

func (f fixture) newEpisode(t *testing.T, seriesID, id string) domain.WorldContext {
	t.Helper()
	_, err := f.series.CreateEpisode(context.Background(), seriesID, "", "", domain.Project{
		ID: id, State: domain.StateDraft, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
		ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced,
	})
	require.NoError(t, err)
	w, err := f.world.World(context.Background(), id, "")
	require.NoError(t, err)
	return w
}

func TestSetProject_SetsReplacesAndRemovesTheReferenceOfAProjectCharacter(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	previous, err := f.repo.SetProject(ctx, "src", "c-src", ref("src/a.wav", "こんにちは"))
	require.NoError(t, err)
	assert.Nil(t, previous)
	w, err := f.world.World(ctx, "src", "")
	require.NoError(t, err)
	assert.Equal(t, ref("src/a.wav", "こんにちは"), w.Characters[0].VoiceReference, "the World State carries the reference")

	previous, err = f.repo.SetProject(ctx, "src", "c-src", ref("src/b.wav", ""))
	require.NoError(t, err)
	assert.Equal(t, "src/a.wav", previous.FilePath, "a replacement tells the file it replaced")
	got, err := f.repo.Project(ctx, "src", "c-src")
	require.NoError(t, err)
	assert.Equal(t, "src/b.wav", got.FilePath)

	previous, err = f.repo.SetProject(ctx, "src", "c-src", nil)
	require.NoError(t, err)
	assert.Equal(t, "src/b.wav", previous.FilePath)
	got, err = f.repo.Project(ctx, "src", "c-src")
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, "bob", mustWorld(t, f, "src").Characters[0].Hairstyle, "the other fields are untouched")
}

func mustWorld(t *testing.T, f fixture, projectID string) domain.WorldContext {
	t.Helper()
	w, err := f.world.World(context.Background(), projectID, "")
	require.NoError(t, err)
	return w
}

func TestSetProject_AnotherProjectsCharacterIsNotFound(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.exec(t, `INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('other', 'auto', 'key_check', 'product_cm', 'balanced')`)

	_, err := f.repo.SetProject(ctx, "other", "c-src", ref("other/a.wav", ""))
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = f.repo.SetProject(ctx, "src", "no-such-character", ref("src/a.wav", ""))
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = f.repo.Project(ctx, "other", "c-src")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	assert.Nil(t, mustWorld(t, f, "src").Characters[0].VoiceReference, "nothing was written")
}

func TestSeries_TheCanonKeepsTheReferenceInEveryVersionAndEpisodesCopyIt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	_, err := f.repo.SetProject(ctx, "src", "c-src", ref("src/a.wav", "元の声"))
	require.NoError(t, err)

	// POST /series from the Project: the canon takes the reference (version 1).
	s, err := f.series.Create(ctx, "Mio Diaries", "src")
	require.NoError(t, err)
	canon, err := f.series.Canon(ctx, s.ID)
	require.NoError(t, err)
	require.Len(t, canon.Characters, 1)
	assert.Equal(t, ref("src/a.wav", "元の声"), canon.Characters[0].VoiceReference)
	v1, err := f.series.CanonAt(ctx, s.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, ref("src/a.wav", "元の声"), v1.Characters[0].VoiceReference, "the version snapshot keeps the reference")

	// A new Episode inherits it: its own row, the file shared.
	w := f.newEpisode(t, s.ID, "ep2")
	require.Len(t, w.Characters, 1)
	assert.Equal(t, ref("src/a.wav", "元の声"), w.Characters[0].VoiceReference)

	// Changing the canon appends a version; the Episode that exists keeps what it copied.
	canonID := canon.Characters[0].ID
	previous, err := f.repo.SetSeries(ctx, s.ID, canonID, ref("series/b.wav", "新しい声"))
	require.NoError(t, err)
	assert.Equal(t, "src/a.wav", previous.FilePath)
	after, err := f.series.Canon(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, mustSeries(t, f, s.ID).Version)
	assert.Equal(t, "series/b.wav", after.Characters[0].VoiceReference.FilePath)
	v2, err := f.series.CanonAt(ctx, s.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, "series/b.wav", v2.Characters[0].VoiceReference.FilePath)
	v1, err = f.series.CanonAt(ctx, s.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "src/a.wav", v1.Characters[0].VoiceReference.FilePath, "an older version is not rewritten")
	assert.Equal(t, "src/a.wav", mustWorld(t, f, "ep2").Characters[0].VoiceReference.FilePath, "an existing Episode is not changed")
	assert.Equal(t, "src/a.wav", mustWorld(t, f, "src").Characters[0].VoiceReference.FilePath)

	// The next Episode gets the new voice.
	assert.Equal(t, "series/b.wav", f.newEpisode(t, s.ID, "ep3").Characters[0].VoiceReference.FilePath)

	// An Episode replaces its voice alone.
	ep2Char := mustWorld(t, f, "ep2").Characters[0].ID
	_, err = f.repo.SetProject(ctx, "ep2", ep2Char, ref("ep2/own.wav", "この話だけ"))
	require.NoError(t, err)
	assert.Equal(t, "ep2/own.wav", mustWorld(t, f, "ep2").Characters[0].VoiceReference.FilePath)
	assert.Equal(t, "series/b.wav", mustWorld(t, f, "ep3").Characters[0].VoiceReference.FilePath)
	still, err := f.repo.Series(ctx, s.ID, canonID)
	require.NoError(t, err)
	assert.Equal(t, "series/b.wav", still.FilePath)

	// Removing the canon voice is a version too, and the Episodes keep theirs.
	_, err = f.repo.SetSeries(ctx, s.ID, canonID, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, mustSeries(t, f, s.ID).Version)
	assert.Nil(t, mustCanon(t, f, s.ID).Characters[0].VoiceReference)
	assert.Equal(t, "series/b.wav", mustWorld(t, f, "ep3").Characters[0].VoiceReference.FilePath)
}

func mustSeries(t *testing.T, f fixture, id string) domain.Series {
	t.Helper()
	s, err := f.series.Get(context.Background(), id)
	require.NoError(t, err)
	return s
}

func mustCanon(t *testing.T, f fixture, id string) domain.SeriesCanon {
	t.Helper()
	c, err := f.series.Canon(context.Background(), id)
	require.NoError(t, err)
	return c
}

func TestSetSeries_UnknownSeriesOrCharacterIsNotFoundAndWritesNoVersion(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	s, err := f.series.Create(ctx, "Mio Diaries", "src")
	require.NoError(t, err)
	canonID := mustCanon(t, f, s.ID).Characters[0].ID

	_, err = f.repo.SetSeries(ctx, "no-such-series", canonID, ref("x/a.wav", ""))
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = f.repo.SetSeries(ctx, s.ID, "c-src", ref("x/a.wav", ""))
	assert.ErrorIs(t, err, repository.ErrNotFound, "a Project's row is not a canonical Character")
	_, err = f.repo.Series(ctx, s.ID, "c-src")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	assert.Equal(t, 1, mustSeries(t, f, s.ID).Version)
}

func TestBranch_ACopyOfTheCharacterKeepsTheCurrentReference(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.exec(t, `INSERT INTO scene (id, project_id, order_index) VALUES ('sc', 'src', 0)`)
	f.exec(t, `INSERT INTO shot (id, scene_id, order_index) VALUES ('sh', 'sc', 0)`)
	_, err := f.repo.SetProject(ctx, "src", "c-src", ref("src/a.wav", "いま"))
	require.NoError(t, err)

	branches := repository.NewBranchRepository(f.db)
	b, err := branches.Create(ctx, repository.NewBranch{ShotID: "sh", Name: "if", PointType: domain.BranchAtShot})
	require.NoError(t, err)

	w, err := f.world.World(ctx, "src", b.ID)
	require.NoError(t, err)
	require.Len(t, w.Characters, 1)
	assert.Equal(t, "src/a.wav", w.Characters[0].VoiceReference.FilePath)
}

func TestInUse_AFileIsInUseWhileAnyRowRefersToIt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	_, err := f.repo.SetProject(ctx, "src", "c-src", ref("src/a.wav", ""))
	require.NoError(t, err)

	used, err := f.repo.InUse(ctx, "src/a.wav")
	require.NoError(t, err)
	assert.True(t, used)
	used, err = f.repo.InUse(ctx, "src/other.wav")
	require.NoError(t, err)
	assert.False(t, used)

	_, err = f.repo.SetProject(ctx, "src", "c-src", nil)
	require.NoError(t, err)
	used, err = f.repo.InUse(ctx, "src/a.wav")
	require.NoError(t, err)
	assert.False(t, used)
}

// beforeVoiceReference is the migration before 20261101130000_character_voice_reference.sql.
const beforeVoiceReference = 20261031120000

// The migration only adds a nullable column: the rows that exist keep every value, and going down drops it again.
func TestMigration_VoiceReferenceKeepsTheCharactersAndIsReversible(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, beforeVoiceReference)
	require.NoError(t, err)
	for _, q := range []string{
		`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p', 'auto', 'key_check', 'product_cm', 'balanced')`,
		`INSERT INTO series (id, name) VALUES ('s', 'S')`,
		`INSERT INTO character (id, project_id, name, hairstyle, voice) VALUES ('c', 'p', 'Mio', 'bob', 'low')`,
		`INSERT INTO character (id, series_id, name, hairstyle) VALUES ('cs', 's', 'Mio', 'bob')`,
	} {
		_, err := db.ExecContext(ctx, q)
		require.NoError(t, err, q)
	}

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	count := func(q string) int {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, q).Scan(&n))
		return n
	}
	assert.Equal(t, 2, count(`SELECT COUNT(*) FROM character WHERE voice_reference IS NULL`), "existing characters have no reference")
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM character WHERE id = 'c' AND hairstyle = 'bob' AND voice = 'low'`), "their values are kept")
	_, err = db.ExecContext(ctx, `UPDATE character SET voice_reference = '{"file_path":"p/a.wav"}' WHERE id = 'c'`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE character SET voice_reference = 'not json' WHERE id = 'cs'`)
	assert.Error(t, err, "the column holds JSON only")
	assert.Zero(t, count(`SELECT COUNT(*) FROM pragma_foreign_key_check`))

	_, err = provider.DownTo(ctx, beforeVoiceReference)
	require.NoError(t, err)
	assert.Equal(t, 2, count(`SELECT COUNT(*) FROM character`))
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM character WHERE id = 'c' AND hairstyle = 'bob' AND voice = 'low'`))
	assert.Zero(t, count(`SELECT COUNT(*) FROM pragma_table_info('character') WHERE name = 'voice_reference'`))
}
