package audioin_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/audioin"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// Every kind is stored under the extension of the kind that was read from its bytes, not of anything the client said.
func TestStageStoresTheBytesUnderTheExtensionOfTheirKindNotOfAnythingTheClientSaid(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		mime string
		file string
	}{
		{"wav", gentesttest.WAV, audiofile.WAV, "id0001.wav"},
		{"mp3", audiofiletest.MP3(false, 2), audiofile.MP3, "id0001.mp3"},
		{"flac", audiofiletest.FLAC(44100, 44100), audiofile.FLAC, "id0001.flac"},
		{"ogg vorbis", audiofiletest.OGGVorbis(44100, 44100), audiofile.OGG, "id0001.ogg"},
		{"ogg opus", audiofiletest.OGGOpus(312, 48000), audiofile.OGG, "id0001.ogg"},
		{"m4a", audiofiletest.M4A(0, 44100, 44100, true), audiofile.M4A, "id0001.m4a"},
		{"m4a of the brand isom (an Android recorder's file)", audiofiletest.MP4WithBrand("isom", 44100, 44100, "soun"), audiofile.M4A, "id0001.m4a"},
		{"m4a of the brand mp42 (ffmpeg's file)", audiofiletest.MP4WithBrand("mp42", 44100, 44100, "soun"), audiofile.M4A, "id0001.m4a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "staging")
			id, mime, err := audioin.Stage(dir, c.data, func() string { return "id0001" })
			require.NoError(t, err)
			assert.Equal(t, c.file, id)
			assert.Equal(t, c.mime, mime)
			got, err := os.ReadFile(filepath.Join(dir, id))
			require.NoError(t, err)
			assert.Equal(t, c.data, got)
		})
	}
}

func TestStageRefusesAnEmptyOversizedOrNotAudioFileAndWritesNothing(t *testing.T) {
	huge := append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, audiofile.MaxBytes)...) // 12 bytes over the limit
	atLimit := append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, audiofile.MaxBytes-12)...)
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"empty", nil, "1 バイト以上 50 MiB 以下"},
		{"over the limit", huge, "1 バイト以上 50 MiB 以下"},
		{"a png", gentesttest.PNG, "WAV・MP3・FLAC・OGG・M4A のどれか"},
		{"a text", []byte(strings.Repeat("x", 100)), "WAV・MP3・FLAC・OGG・M4A のどれか"},
		{"an ogg of video (Theora)", audiofiletest.OGGTheora(), audioin.NotAudioMessage},
		{"an mp4 video of the brand mp42 (sound and video tracks)", audiofiletest.MP4WithBrand("mp42", 1000, 5000, "soun", "vide"), audioin.NotAudioMessage},
		{"an mp4 video of the brand isom (a video track)", audiofiletest.MP4WithBrand("isom", 1000, 5000, "vide"), audioin.NotAudioMessage},
		{"an mp4 of the brand isom that has no moov box", audiofiletest.MP4NoMoov("isom"), audioin.NotAudioMessage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "staging")
			id, _, err := audioin.Stage(dir, c.data, func() string { return "id0001" })
			var invalid *gterr.InvalidError
			require.True(t, errors.As(err, &invalid), "%v", err)
			assert.Contains(t, invalid.Message, c.want)
			assert.Empty(t, id)
			_, statErr := os.Stat(dir)
			assert.True(t, os.IsNotExist(statErr), "not even the directory is made")
		})
	}
	// the limit itself is allowed
	_, _, err := audioin.Stage(filepath.Join(t.TempDir(), "staging"), atLimit, func() string { return "id0001" })
	assert.NoError(t, err)
}

// The sentence every entrance refuses a file with names all five kinds, so that a user is told the same everywhere.
func TestTheRefusalNamesEveryKind(t *testing.T) {
	assert.Equal(t, "音声ファイルは WAV・MP3・FLAC・OGG・M4A のどれかにしてください", audioin.NotAudioMessage)
}
