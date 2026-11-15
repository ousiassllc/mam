package audioin_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/audioin"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// mp3Frame is the header of an MPEG-1 layer III frame (128 kbit/s, 44.1 kHz): what an MP3 without an ID3 tag starts with.
var mp3Frame = []byte{0xFF, 0xFB, 0x90, 0x00, 0x00, 0x00}

func TestSniffReadsTheKindFromTheBytes(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		mime string
	}{
		{"wav", gentesttest.WAV, audioin.WAV},
		{"mp3 with an ID3 tag", []byte("ID3\x04\x00\x00\x00\x00\x00\x00"), audioin.MP3},
		{"mp3 with a bare frame header", mp3Frame, audioin.MP3},
		{"mp3 at 2 bytes short of a header", mp3Frame[:2], ""},
		{"png", gentesttest.PNG, ""},
		{"text", []byte("hello, this is not a sound"), ""},
		{"flac", []byte("fLaC\x00\x00\x00\x22"), ""},
		{"riff that is not wave (an AVI)", []byte("RIFF\x00\x00\x00\x00AVI LIST"), ""},
		{"aac in an ADTS frame (layer bits 00)", []byte{0xFF, 0xF1, 0x50, 0x80, 0x00}, ""},
		{"mpeg audio layer II", []byte{0xFF, 0xFD, 0x90, 0x00}, ""},
		{"mpeg audio with the reserved version", []byte{0xFF, 0xEB, 0x90, 0x00}, ""},
		{"mpeg audio with the bad bitrate", []byte{0xFF, 0xFB, 0xF0, 0x00}, ""},
		{"mpeg audio with the reserved sample rate", []byte{0xFF, 0xFB, 0x9C, 0x00}, ""},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mime, ok := audioin.Sniff(c.data)
			assert.Equal(t, c.mime != "", ok)
			assert.Equal(t, c.mime, mime)
		})
	}
}

func TestTheTypeAndTheExtensionOfAKindAreOneContract(t *testing.T) {
	for mime, ext := range map[string]string{audioin.WAV: ".wav", audioin.MP3: ".mp3"} {
		assert.True(t, audioin.Is(mime))
		assert.Equal(t, ext, audioin.Ext(mime))
		back, ok := audioin.MimeOfExt(ext)
		assert.True(t, ok)
		assert.Equal(t, mime, back)
	}
	for _, mime := range []string{"image/png", "audio/flac", "audio/ogg", ""} {
		assert.False(t, audioin.Is(mime), mime)
		assert.Empty(t, audioin.Ext(mime), mime)
	}
	for _, ext := range []string{".png", ".flac", ".WAV", ""} {
		_, ok := audioin.MimeOfExt(ext)
		assert.False(t, ok, ext)
	}
}

func TestStageStoresTheBytesUnderTheExtensionOfTheirKindNotOfAnythingTheClientSaid(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "staging")
	next := func() string { return "id0001" }
	id, mime, err := audioin.Stage(dir, gentesttest.WAV, next)
	require.NoError(t, err)
	assert.Equal(t, "id0001.wav", id)
	assert.Equal(t, audioin.WAV, mime)
	got, err := os.ReadFile(filepath.Join(dir, id))
	require.NoError(t, err)
	assert.Equal(t, gentesttest.WAV, got)

	id, mime, err = audioin.Stage(dir, mp3Frame, func() string { return "id0002" })
	require.NoError(t, err)
	assert.Equal(t, "id0002.mp3", id)
	assert.Equal(t, audioin.MP3, mime)
}

func TestStageRefusesAnEmptyOversizedOrNotAudioFileAndWritesNothing(t *testing.T) {
	huge := append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, audioin.MaxBytes)...) // 12 bytes over the limit
	atLimit := append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, audioin.MaxBytes-12)...)
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"empty", nil, "1 バイト以上 50 MiB 以下"},
		{"over the limit", huge, "1 バイト以上 50 MiB 以下"},
		{"a png", gentesttest.PNG, "WAV か MP3"},
		{"a text", []byte(strings.Repeat("x", 100)), "WAV か MP3"},
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
