package voicerefsvc_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

// fakeProbe is ffprobe: it reports the length the test says, or fails.
type fakeProbe struct {
	seconds float64
	err     error
}

func (p *fakeProbe) AudioDuration(context.Context, string) (float64, error) { return p.seconds, p.err }

type fixture struct {
	t      *testing.T
	db     *repository.DB
	assets string
	probe  *fakeProbe
	svc    *voicerefsvc.Service
	series *repository.SeriesRepository
	world  *repository.WorldRepository
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testdb.Open(t)
	f := &fixture{t: t, db: db, assets: t.TempDir(), probe: &fakeProbe{seconds: 5}}
	f.series, f.world = repository.NewSeriesRepository(db), repository.NewWorldRepository(db)
	f.svc = voicerefsvc.New(voicerefsvc.Deps{
		DB: db, Series: f.series, Projects: repository.NewProjectRepository(db), World: f.world, AssetsDir: f.assets, Probe: f.probe,
	})
	f.exec(`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'key_check', 'product_cm', 'balanced')`)
	f.exec(`INSERT INTO character (id, project_id, name) VALUES ('c1', 'p1', 'ユイ')`)
	return f
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	require.NoError(f.t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// wav is a PCM WAV of the given number of samples at 8 kHz.
func wav(samples int) []byte {
	var b bytes.Buffer
	data := make([]byte, samples*2)
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)})
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}

func file(name string, data []byte) voicerefsvc.File {
	return voicerefsvc.File{Name: name, Size: int64(len(data)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}
}

func ptr(s string) *string { return &s }

func voiceInput(data []byte, text string) voicerefsvc.Input {
	return voicerefsvc.Input{File: file("voice.wav", data), Text: ptr(text)}
}

func (f *fixture) files() []string {
	f.t.Helper()
	var out []string
	require.NoError(f.t, filepath.WalkDir(f.assets, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(f.assets, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return err
	}))
	return out
}

func invalidField(t *testing.T, err error) string {
	t.Helper()
	var inv *voicerefsvc.InvalidError
	require.ErrorAs(t, err, &inv, "an invalid input is an *InvalidError")
	return inv.Field
}

func TestSetForProject_StoresTheRecordingWithItsTranscriptAndLanguage(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	ref, err := f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{File: file("voice.WAV", wav(40000)), Text: ptr("  こんにちは  ")})
	require.NoError(t, err)
	assert.Equal(t, "audio/wav", ref.MimeType)
	assert.Equal(t, "ja", ref.Language, "the default language")
	assert.Equal(t, "こんにちは", ref.Text)
	assert.Equal(t, 5000, ref.DurationMS)
	assert.True(t, strings.HasPrefix(ref.FilePath, "p1/") && strings.HasSuffix(ref.FilePath, ".wav"), ref.FilePath)
	assert.EqualValues(t, len(wav(40000)), ref.SizeBytes)
	assert.Equal(t, []string{ref.FilePath}, f.files())

	w, err := f.world.World(ctx, "p1", "")
	require.NoError(t, err)
	assert.Equal(t, &ref, w.Characters[0].VoiceReference)

	play, err := f.svc.PlaybackForProject(ctx, "p1", "c1")
	require.NoError(t, err)
	assert.Equal(t, "audio/wav", play.MimeType)
	assert.Equal(t, filepath.Join(f.assets, filepath.FromSlash(ref.FilePath)), play.Path)
}

func TestSetForProject_RefusesWhatIsNotAReferenceVoiceAndStoresNothing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	big := make([]byte, voiceref.MaxBytes+1)
	copy(big, wav(1))
	cases := map[string]struct {
		in    voicerefsvc.Input
		field string
	}{
		"no file":             {voicerefsvc.Input{}, "file"},
		"empty":               {voicerefsvc.Input{File: file("a.wav", nil)}, "file"},
		"unsupported type":    {voicerefsvc.Input{File: file("a.exe", wav(10))}, "file"},
		"renamed executable":  {voicerefsvc.Input{File: file("a.wav", []byte("MZ\x90\x00 not audio"))}, "file"},
		"mp3 named wav":       {voicerefsvc.Input{File: file("a.mp3", wav(10))}, "file"},
		"over the size limit": {voicerefsvc.Input{File: file("a.wav", big)}, "file"},
		"unknown language":    {voicerefsvc.Input{File: file("a.wav", wav(10)), Language: ptr("xx")}, "language"},
		"transcript too long": {voicerefsvc.Input{File: file("a.wav", wav(10)), Text: ptr(strings.Repeat("あ", voiceref.MaxTextRunes+1))}, "text"},
	}
	for name, c := range cases {
		_, err := f.svc.SetForProject(ctx, "p1", "c1", c.in)
		assert.Equal(t, c.field, invalidField(t, err), name)
	}
	assert.Empty(t, f.files(), "a refused recording leaves no file")
	w, err := f.world.World(ctx, "p1", "")
	require.NoError(t, err)
	assert.Nil(t, w.Characters[0].VoiceReference)
}

func TestSetForProject_TheLengthMustBeThreeToTenSecondsWhenItCanBeMeasured(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	in := voicerefsvc.Input{File: file("a.wav", wav(100))}

	for _, seconds := range []float64{2.99, 10.01, 0.5, 60} {
		f.probe.seconds = seconds
		_, err := f.svc.SetForProject(ctx, "p1", "c1", in)
		assert.Equal(t, "file", invalidField(t, err), "%v s", seconds)
		assert.Contains(t, err.Error(), "3〜10 秒")
	}
	assert.Empty(t, f.files())

	for _, seconds := range []float64{3, 10, 6.5} {
		f.probe.seconds = seconds
		ref, err := f.svc.SetForProject(ctx, "p1", "c1", in)
		require.NoError(t, err, "%v s", seconds)
		assert.Equal(t, int(seconds*1000), ref.DurationMS)
	}

	f.probe.seconds, f.probe.err = 5, errors.New("ffprobe: Invalid data found")
	_, err := f.svc.SetForProject(ctx, "p1", "c1", in)
	assert.Equal(t, "file", invalidField(t, err), "something ffprobe cannot read is not audio")
	assert.Len(t, f.files(), 1, "only the last accepted recording remains")
}

func TestSetForProject_WithoutFFprobeTheLengthIsNotCheckedAndIsLeftUnmeasured(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.svc = voicerefsvc.New(voicerefsvc.Deps{DB: f.db, Series: f.series, Projects: repository.NewProjectRepository(f.db), World: f.world, AssetsDir: f.assets})

	ref, err := f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{File: file("a.wav", wav(100))})
	require.NoError(t, err)
	assert.Zero(t, ref.DurationMS, "0 = not measured")
}

func TestSetForProject_ReplacingRemovesTheOldFileAndNewRecordingDropsTheOldTranscript(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	first, err := f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{File: file("a.wav", wav(100)), Text: ptr("前の声"), Language: ptr("en")})
	require.NoError(t, err)

	second, err := f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{File: file("b.flac", append([]byte("fLaC"), make([]byte, 40)...))})
	require.NoError(t, err)
	assert.Equal(t, "audio/flac", second.MimeType)
	assert.Empty(t, second.Text, "the transcript of the old recording does not describe the new one")
	assert.Equal(t, "en", second.Language, "the language stays")
	assert.Equal(t, []string{second.FilePath}, f.files(), "the replaced file is removed")
	assert.NotEqual(t, first.FilePath, second.FilePath)
}

func TestSetForProject_WithoutAFileItChangesTheTranscriptAndLanguageOfTheExistingVoice(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	_, err := f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{File: file("a.wav", wav(100)), Text: ptr("old")})
	require.NoError(t, err)

	ref, err := f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{Text: ptr("new"), Language: ptr("EN")})
	require.NoError(t, err)
	assert.Equal(t, "new", ref.Text)
	assert.Equal(t, "en", ref.Language)
	assert.Len(t, f.files(), 1, "the recording is kept")

	ref, err = f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{})
	require.NoError(t, err)
	assert.Equal(t, "new", ref.Text, "nothing sent, nothing changed")

	f.exec(`INSERT INTO character (id, project_id, name) VALUES ('c2', 'p1', 'ミオ')`)
	_, err = f.svc.SetForProject(ctx, "p1", "c2", voicerefsvc.Input{Text: ptr("x")})
	assert.Equal(t, "file", invalidField(t, err), "a Character that has no voice needs a recording first")
}

func TestSetForProject_UnknownProjectOrCharacterIsNotFoundAndLeavesNoFile(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	in := voicerefsvc.Input{File: file("a.wav", wav(100))}

	_, err := f.svc.SetForProject(ctx, "nope", "c1", in)
	assert.ErrorIs(t, err, voicerefsvc.ErrNotFound)
	_, err = f.svc.SetForProject(ctx, "p1", "nope", in)
	assert.ErrorIs(t, err, voicerefsvc.ErrNotFound)
	f.exec(`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p2', 'auto', 'key_check', 'product_cm', 'balanced')`)
	_, err = f.svc.SetForProject(ctx, "p2", "c1", in)
	assert.ErrorIs(t, err, voicerefsvc.ErrNotFound, "another Project's Character")
	_, err = f.svc.SetForSeries(ctx, "nope", "c1", in)
	assert.ErrorIs(t, err, voicerefsvc.ErrNotFound)
	assert.ErrorIs(t, f.svc.RemoveForProject(ctx, "p1", "nope"), voicerefsvc.ErrNotFound)
	_, err = f.svc.PlaybackForProject(ctx, "p1", "c1")
	assert.ErrorIs(t, err, voicerefsvc.ErrNotFound, "a Character without a voice has nothing to play")
	assert.Empty(t, f.files())
}

func TestRemove_TakesTheVoiceAwayAndKeepsTheFileAnotherRowStillUses(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ref, err := f.svc.SetForProject(ctx, "p1", "c1", voicerefsvc.Input{File: file("a.wav", wav(100))})
	require.NoError(t, err)
	// A Branch copy of the Character shares the recording.
	f.exec(`INSERT INTO character (id, project_id, name, voice_reference) SELECT 'c1b', 'p1', 'ユイ', voice_reference FROM character WHERE id = 'c1'`)

	require.NoError(t, f.svc.RemoveForProject(ctx, "p1", "c1"))
	assert.Equal(t, []string{ref.FilePath}, f.files(), "another row still uses the file")
	require.NoError(t, f.svc.RemoveForProject(ctx, "p1", "c1b"))
	assert.Empty(t, f.files(), "nothing uses it any more")
	require.NoError(t, f.svc.RemoveForProject(ctx, "p1", "c1"), "removing nothing is not an error")
	w, err := f.world.World(ctx, "p1", "")
	require.NoError(t, err)
	for _, c := range w.Characters {
		assert.Nil(t, c.VoiceReference)
	}
}

func TestSeries_TheCanonOwnsTheVoiceAndEpisodesInheritItWithTheFileShared(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	s, err := f.series.Create(ctx, "ユイの日常", "p1")
	require.NoError(t, err)
	canon, err := f.series.Canon(ctx, s.ID)
	require.NoError(t, err)
	canonID := canon.Characters[0].ID

	ref, err := f.svc.SetForSeries(ctx, s.ID, canonID, voicerefsvc.Input{File: file("canon.mp3", append([]byte("ID3"), make([]byte, 40)...)), Text: ptr("正本の声"), Language: ptr("ja")})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(ref.FilePath, s.ID+"/"), "a canon recording lives in the directory of the Series: %s", ref.FilePath)
	assert.Equal(t, 2, mustVersion(t, f, s.ID), "setting the voice of the canon is a new version")

	_, err = f.series.CreateEpisode(ctx, s.ID, "", "", domain.Project{
		ID: "ep2", State: domain.StateDraft, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
		ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced,
	})
	require.NoError(t, err)
	inherited, err := f.svc.ForSpeaker(ctx, "ep2", "", "ユイ")
	require.NoError(t, err)
	assert.Equal(t, ref, inherited.Reference, "the Episode's Character has the same voice")
	assert.Equal(t, filepath.Join(f.assets, filepath.FromSlash(ref.FilePath)), inherited.Path, "the file is shared")

	// The canon changes afterwards: the Episode keeps the old recording, and the old file is kept for it.
	newer, err := f.svc.SetForSeries(ctx, s.ID, canonID, voicerefsvc.Input{File: file("v2.wav", wav(100))})
	require.NoError(t, err)
	still, err := f.svc.ForSpeaker(ctx, "ep2", "", "ユイ")
	require.NoError(t, err)
	assert.Equal(t, ref.FilePath, still.Reference.FilePath)
	assert.ElementsMatch(t, []string{ref.FilePath, newer.FilePath}, f.files())

	// The Episode replaces its voice alone; the file it had is then unused and goes.
	epChar := inherited.Character.ID
	own, err := f.svc.SetForProject(ctx, "ep2", epChar, voicerefsvc.Input{File: file("own.wav", wav(100))})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{newer.FilePath, own.FilePath}, f.files())
	play, err := f.svc.PlaybackForSeries(ctx, s.ID, canonID)
	require.NoError(t, err)
	assert.Equal(t, "audio/wav", play.MimeType)

	require.NoError(t, f.svc.RemoveForSeries(ctx, s.ID, canonID))
	assert.Equal(t, []string{own.FilePath}, f.files())
	_, err = f.svc.PlaybackForSeries(ctx, s.ID, canonID)
	assert.ErrorIs(t, err, voicerefsvc.ErrNotFound)
}

func mustVersion(t *testing.T, f *fixture, seriesID string) int {
	t.Helper()
	s, err := f.series.Get(context.Background(), seriesID)
	require.NoError(t, err)
	return s.Version
}
