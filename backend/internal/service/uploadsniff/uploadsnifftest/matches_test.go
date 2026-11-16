package uploadsnifftest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/uploadsniff"
)

var (
	png = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	mp4 = append([]byte("\x00\x00\x00\x18ftypisom"), make([]byte, 32)...)
	mov = append([]byte("\x00\x00\x00\x14ftypqt  "), make([]byte, 32)...)
)

// The bytes must be the kind the extension names, for every kind of attachment: an image, a video and (Issue #1153) each of the audio kinds.
func TestMatches_TheBytesMustBeTheKindTheExtensionNames(t *testing.T) {
	audio := []struct {
		ext, mime string
		data      []byte
	}{
		{".wav", audiofile.WAV, audiofiletest.WAV(16000, 16000)},
		{".mp3", audiofile.MP3, audiofiletest.MP3(true, 20)},
		{".flac", audiofile.FLAC, audiofiletest.FLAC(44100, 44100)},
		{".ogg", audiofile.OGG, audiofiletest.OGGVorbis(44100, 44100)},
		{".ogg", audiofile.OGG, audiofiletest.OGGOpus(312, 48000)},
		{".m4a", audiofile.M4A, audiofiletest.M4A(0, 44100, 44100, true)},
	}
	for _, a := range audio {
		assert.True(t, uploadsniff.Matches(a.ext, domain.AssetAudio, a.mime, a.data), a.ext)
		for _, other := range audio {
			if other.mime != a.mime {
				assert.False(t, uploadsniff.Matches(a.ext, domain.AssetAudio, a.mime, other.data), "%s file with %s bytes", a.ext, other.ext)
			}
		}
		assert.False(t, uploadsniff.Matches(a.ext, domain.AssetAudio, a.mime, png), "an image named %s", a.ext)
		assert.False(t, uploadsniff.Matches(a.ext, domain.AssetAudio, a.mime, mp4), "a video named %s", a.ext)
		assert.False(t, uploadsniff.Matches(a.ext, domain.AssetAudio, a.mime, nil), "nothing named %s", a.ext)
		assert.False(t, uploadsniff.Matches(".png", domain.AssetImage, "image/png", a.data), "audio bytes named png")
		// A video upload is checked by its container signature only (the tracks are never read), so an audio-only ISO file (M4A) named .mp4 is an MP4 container
		// and is stored as a video (played by <video>, never used as audio); every other audio kind is no MP4.
		assert.Equal(t, a.mime == audiofile.M4A, uploadsniff.Matches(".mp4", domain.AssetVideo, "video/mp4", a.data), "audio bytes named mp4: %s", a.ext)
	}
	assert.False(t, uploadsniff.Matches(".m4a", domain.AssetAudio, audiofile.M4A, audiofiletest.MP4WithBrand("isom", 44100, 44100)), "an MP4 video is no M4A")

	assert.True(t, uploadsniff.Matches(".png", domain.AssetImage, "image/png", png))
	assert.False(t, uploadsniff.Matches(".jpg", domain.AssetImage, "image/jpeg", png))
	assert.True(t, uploadsniff.Matches(".mp4", domain.AssetVideo, "video/mp4", mp4))
	assert.False(t, uploadsniff.Matches(".mp4", domain.AssetVideo, "video/mp4", mov), "a QuickTime body is no .mp4")
	assert.True(t, uploadsniff.Matches(".mov", domain.AssetVideo, "video/quicktime", mov))
	assert.True(t, uploadsniff.Matches(".webm", domain.AssetVideo, "video/webm", []byte{0x1A, 0x45, 0xDF, 0xA3, 0}))
	assert.False(t, uploadsniff.Matches(".txt", domain.AssetVideo, "text/plain", []byte("hello")), "an extension that names no kind")
}
