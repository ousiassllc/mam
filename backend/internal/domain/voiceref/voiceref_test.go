package voiceref_test

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

func TestReference_RoundTripsThroughAJSONColumnAndNullIsNil(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE c (id TEXT, voice_reference TEXT)`)
	require.NoError(t, err)

	ref := &voiceref.Reference{FilePath: "p/a.wav", MimeType: "audio/wav", SizeBytes: 9, DurationMS: 4200, Text: "こんにちは", Language: "ja"}
	_, err = db.Exec(`INSERT INTO c VALUES ('with', ?), ('without', ?)`, ref, (*voiceref.Reference)(nil))
	require.NoError(t, err)

	var with, without *voiceref.Reference
	require.NoError(t, db.QueryRow(`SELECT voice_reference FROM c WHERE id = 'with'`).Scan(&with))
	require.NoError(t, db.QueryRow(`SELECT voice_reference FROM c WHERE id = 'without'`).Scan(&without))
	assert.Equal(t, ref, with)
	assert.Nil(t, without, "a NULL column is no reference")
	assert.Equal(t, 4200*time.Millisecond, with.Duration())

	var raw sql.NullString
	require.NoError(t, db.QueryRow(`SELECT voice_reference FROM c WHERE id = 'without'`).Scan(&raw))
	assert.False(t, raw.Valid, "no reference is stored as NULL, not as 'null'")
}

func TestReference_ScanRefusesWhatIsNotAReference(t *testing.T) {
	var r voiceref.Reference
	assert.Error(t, r.Scan(42))
	assert.Error(t, r.Scan("not json"))
	assert.NoError(t, r.Scan([]byte(`{"file_path":"p/a.wav","language":"en"}`)))
	assert.Equal(t, "en", r.Language)
}

func TestFormatOf_TheContentMustMatchTheExtension(t *testing.T) {
	wav := []byte("RIFF\x24\x00\x00\x00WAVEfmt ")
	cases := []struct {
		ext  string
		head []byte
		ok   bool
		mime string
	}{
		{".wav", wav, true, "audio/wav"},
		{".WAV", wav, true, "audio/wav"},
		{".mp3", []byte("ID3\x04\x00"), true, "audio/mpeg"},
		{".mp3", []byte{0xFF, 0xFB, 0x90, 0x00}, true, "audio/mpeg"},
		{".flac", []byte("fLaC\x00\x00"), true, "audio/flac"},
		{".ogg", []byte("OggS\x00\x02"), true, "audio/ogg"},
		{".m4a", []byte("\x00\x00\x00\x20ftypM4A \x00\x00"), true, "audio/mp4"},
		{".wav", []byte("MZ\x90\x00 an executable"), false, ""},
		{".mp3", wav, false, ""},
		{".wav", []byte("RIFF"), false, ""},
		{".wav", nil, false, ""},
	}
	for _, c := range cases {
		f, known := voiceref.FormatOf(c.ext)
		require.True(t, known, c.ext)
		assert.Equal(t, c.ok, f.Matches(c.head), "%s %q", c.ext, c.head)
		if c.ok {
			assert.Equal(t, c.mime, f.MimeType)
		}
	}
	_, known := voiceref.FormatOf(".exe")
	assert.False(t, known)
	assert.Equal(t, []string{".wav", ".mp3", ".flac", ".ogg", ".m4a"}, voiceref.Extensions())
}

func TestNormalizeLanguageAndText(t *testing.T) {
	for in, want := range map[string]string{"": "ja", " JA ": "ja", "en": "en", "yue": "yue"} {
		got, err := voiceref.NormalizeLanguage(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got)
	}
	_, err := voiceref.NormalizeLanguage("fr")
	assert.ErrorIs(t, err, voiceref.ErrLanguage)

	got, err := voiceref.NormalizeText("  こんにちは  ")
	require.NoError(t, err)
	assert.Equal(t, "こんにちは", got)
	_, err = voiceref.NormalizeText(strings.Repeat("あ", voiceref.MaxTextRunes))
	assert.NoError(t, err)
	_, err = voiceref.NormalizeText(strings.Repeat("あ", voiceref.MaxTextRunes+1))
	assert.ErrorIs(t, err, voiceref.ErrText)
}
