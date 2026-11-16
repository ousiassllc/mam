package audiofiletest

import (
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
)

func fixtures() map[string][]byte {
	return map[string][]byte{
		"wav":         WAV(8000, 800),
		"mp3 id3":     MP3(true, 4),
		"mp3 mpeg2":   MP3MPEG2(6),
		"flac":        FLAC(44100, 441000),
		"ogg vorbis":  OGGVorbis(44100, 88200),
		"ogg opus":    OGGOpus(312, 96000),
		"m4a v0":      M4A(0, 44100, 220500, false),
		"m4a v1":      M4A(1, 1000, 12345, true),
		"id3 + flac":  append(MP3(true, 0), FLAC(44100, 44100)...),
		"ogg theora":  OGGTheora(),
		"mp4 isom":    MP4WithBrand("isom", 1000, 5000, "vide"),
		"isom audio":  MP4WithBrand("isom", 44100, 88200, "soun"),
		"mp42 av":     MP4WithBrand("mp42", 1000, 5000, "soun", "vide"),
		"isom last":   MP4MoovLast("isom", 44100, 88200, "soun"),
		"png":         png,
		"text of ogg": []byte("OggS is the name of a page"),
	}
}

// check calls Sniff and Duration and holds them to their contract: no panic, and either an error that is ErrUnreadable and a zero length, or a length above 0.
func check(t *testing.T, name string, data []byte) {
	t.Helper()
	audiofile.Sniff(data)
	d, err := audiofile.Duration(data)
	if err != nil {
		assert.True(t, errors.Is(err, audiofile.ErrUnreadable), "%s: %v", name, err)
		assert.Zero(t, d, name)
		return
	}
	assert.Positive(t, d, name)
}

// A file cut at any length, as an upload that broke off is, ends in an error or in the length of what is left; never in a panic or an endless loop.
func TestEveryTruncationOfEveryFixtureEndsInAnErrorOrALength(t *testing.T) {
	for name, data := range fixtures() {
		for n := 0; n <= len(data); n++ {
			check(t, name, data[:n])
		}
	}
}

// Bytes of a fixture overwritten at random (a fixed seed: the same cases every run) end the same way.
func TestRandomCorruptionOfEveryFixtureEndsInAnErrorOrALength(t *testing.T) {
	rng := rand.New(rand.NewPCG(1153, 2026))
	for name, data := range fixtures() {
		for range 300 {
			bad := append([]byte{}, data...)
			for range 1 + rng.IntN(4) {
				bad[rng.IntN(len(bad))] = byte(rng.IntN(256))
			}
			check(t, name, bad)
		}
	}
}

// A file of 0xFF bytes (every byte a frame sync), of zeros, and of "OggS" repeated are the worst cases of the searches: they end.
func TestTheWorstCasesOfTheSearchesEnd(t *testing.T) {
	ones := make([]byte, 200<<10)
	for i := range ones {
		ones[i] = 0xFF
	}
	oggs := make([]byte, 0, 200<<10)
	for len(oggs) < 200<<10 {
		oggs = append(oggs, "OggS"...)
	}
	check(t, "0xFF", ones)
	check(t, "zeros", make([]byte, 200<<10))
	check(t, "OggS", oggs)
	check(t, "id3 then 0xFF", append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), ones...))
	check(t, "riff then 0xFF", append([]byte("RIFF\x00\x00\x00\x00WAVE"), ones...))
	check(t, "ogg vorbis then OggS", append(OGGVorbis(44100, 44100), oggs...))
}
