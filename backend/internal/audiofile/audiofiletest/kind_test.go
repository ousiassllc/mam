package audiofiletest

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
)

func TestTheTypeAndTheExtensionOfAKindAreOneContract(t *testing.T) {
	for mime, ext := range map[string]string{
		audiofile.WAV: ".wav", audiofile.MP3: ".mp3", audiofile.FLAC: ".flac", audiofile.OGG: ".ogg", audiofile.M4A: ".m4a",
	} {
		assert.True(t, audiofile.Is(mime), mime)
		assert.Equal(t, ext, audiofile.Ext(mime))
		back, ok := audiofile.MimeOfExt(ext)
		assert.True(t, ok, ext)
		assert.Equal(t, mime, back)
	}
	for _, mime := range []string{"image/png", "audio/aac", "audio/x-wav", "video/mp4", ""} {
		assert.False(t, audiofile.Is(mime), mime)
		assert.Empty(t, audiofile.Ext(mime), mime)
	}
	for _, ext := range []string{".png", ".aac", ".mp4", ".WAV", ""} {
		_, ok := audiofile.MimeOfExt(ext)
		assert.False(t, ok, ext)
	}
}

// What the fixtures say the kind is, Sniff and Duration agree on: the stored extension is the one of the kind that was read.
func TestEveryFixtureIsStoredUnderTheExtensionOfItsKind(t *testing.T) {
	for ext, data := range map[string][]byte{
		".wav": WAV(8000, 80), ".mp3": MP3(true, 3), ".flac": FLAC(44100, 100), ".ogg": OGGVorbis(44100, 100), ".m4a": M4A(0, 1000, 1000, true),
	} {
		mime, ok := audiofile.Sniff(data)
		assert.True(t, ok, ext)
		assert.Equal(t, ext, audiofile.Ext(mime))
	}
}

// The accept attribute of the file input names every kind by type and by extension, in the order of the kinds.
func TestAcceptNamesEveryKindByTypeAndByExtension(t *testing.T) {
	assert.Equal(t, "audio/wav,audio/mpeg,audio/flac,audio/ogg,audio/mp4,.wav,.mp3,.flac,.ogg,.m4a", audiofile.Accept())
}
