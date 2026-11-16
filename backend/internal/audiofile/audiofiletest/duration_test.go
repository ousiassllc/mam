package audiofiletest

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
)

// 100 frames of 1152 samples at 44.1 kHz, and of 576 samples at 22.05 kHz: the same 2.612244897 s.
const hundredFrames = 2612244897 * time.Nanosecond

func TestDurationReadsTheLengthFromEachContainer(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want time.Duration
	}{
		{"wav, a LIST chunk (of an odd size) before the data", WAV(8000, 24000), 3 * time.Second},
		{"wav, 44.1 kHz", WAV(44100, 22050), 500 * time.Millisecond},
		{"mp3, bare frames", MP3(false, 100), hundredFrames},
		{"mp3, an ID3v2 tag before CBR frames", MP3(true, 100), hundredFrames},
		{"mp3, MPEG-2 layer III", MP3MPEG2(100), hundredFrames},
		{"flac, 44.1 kHz", FLAC(44100, 441000), 10 * time.Second},
		{"flac, more samples than 32 bits hold", FLAC(48000, 1<<32+48000), 89479*time.Second + 485333333*time.Nanosecond},
		{"flac under an ID3v2 tag", append(MP3(true, 0), FLAC(44100, 44100)...), time.Second},
		{"ogg vorbis", OGGVorbis(44100, 88200), 2 * time.Second},
		{"ogg opus, the pre-skip is not played", OGGOpus(312, 96000), 2 * time.Second},
		{"m4a, the moov box first", M4A(0, 44100, 220500, true), 5 * time.Second},
		{"m4a, the moov box after the media data", M4A(0, 44100, 220500, false), 5 * time.Second},
		{"m4a, a 64 bit movie header", M4A(1, 1000, 12345, true), 12345 * time.Millisecond},
		{"m4b", MP4WithBrand("M4B ", 600, 36000, "soun"), time.Minute},
		{"isom with a sound track", MP4WithBrand("isom", 600, 36000, "soun"), time.Minute},
		{"mp42 with a sound track", MP4WithBrand("mp42", 1000, 5000, "soun"), 5 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := audiofile.Duration(c.data)
			require.NoError(t, err)
			assert.InDelta(t, float64(c.want), float64(got), float64(time.Microsecond))
		})
	}
}

func TestDurationRefusesWhatHasNoLength(t *testing.T) {
	wavNoFmt := WAV(8000, 80)
	copy(wavNoFmt[12:16], "junk") // the fmt chunk is gone
	wavBadRate := WAV(8000, 80)
	binary.LittleEndian.PutUint32(wavBadRate[28:], 0) // byte rate 0 ...
	binary.LittleEndian.PutUint32(wavBadRate[24:], 0) // ... and sample rate 0
	cases := map[string][]byte{
		"a text":                             []byte("hello, this is not a sound"),
		"a png":                              png,
		"empty":                              nil,
		"wav with no sound":                  WAV(8000, 0),
		"wav with no fmt chunk":              wavNoFmt,
		"wav with no rate":                   wavBadRate,
		"mp3 that is only its ID3 tag":       MP3(true, 0),
		"flac of an unknown length (0)":      FLAC(44100, 0),
		"flac with no sample rate":           FLAC(0, 100),
		"flac that is only its marker":       []byte("fLaC"),
		"ogg opus that ends inside pre-skip": OGGOpus(312, 0),
		"ogg vorbis with no sample rate":     OGGVorbis(0, 100),
		"ogg theora":                         OGGTheora(),
		"m4a without a moov box":             M4A(0, 44100, 100, true)[:20],
		"m4a with a timescale of 0":          M4A(0, 0, 100, true),
		"m4a with a duration of 0":           M4A(0, 1000, 0, true),
		"mp4 video of the brand isom":        MP4WithBrand("isom", 1000, 5000, "vide"),
		"mp42 with sound and video tracks":   MP4WithBrand("mp42", 1000, 5000, "soun", "vide"),
		"isom without a moov box":            MP4NoMoov("isom"),
		"m4a cut inside its mvhd box":        M4A(0, 1000, 5000, true)[:40],
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := audiofile.Duration(data)
			assert.True(t, errors.Is(err, audiofile.ErrUnreadable), "%v", err)
			assert.Zero(t, d)
		})
	}
}

// A WAV a program wrote to a pipe has no sizes (0 or 0xFFFFFFFF): the sound runs to the end of the file. A data chunk that claims more than the file holds is
// the same case as a file that is cut.
func TestDurationOfAWavWhoseSizesAreNotKnownRunsToTheEnd(t *testing.T) {
	for _, size := range []uint32{0, 0xFFFFFFFF, 1 << 20} {
		data := WAV(8000, 16000)
		at := len(data) - 16000*2 - 4
		binary.LittleEndian.PutUint32(data[at:], size)
		got, err := audiofile.Duration(data)
		require.NoError(t, err, size)
		assert.Equal(t, 2*time.Second, got, size)
	}
}

// The last page of an Ogg is the one that says how long it is; a page that is cut, or has no packet ending on it (granule -1), says nothing.
func TestDurationOfAnOggIsTheLastCompletePageThatHasAPosition(t *testing.T) {
	data := OGGVorbis(44100, 88200) // the pages: headers, a page at 44100, one at -1, the last at 88200
	last := lastPageAt(data)
	got, err := audiofile.Duration(data[:last])
	require.NoError(t, err)
	assert.Equal(t, time.Second, got, "without the last page")

	got, err = audiofile.Duration(data[:last+30])
	require.NoError(t, err)
	assert.Equal(t, time.Second, got, "with the last page cut inside its body")

	// the serial number of the stream is the one of the first page: the page of another stream is not its length
	other := append([]byte{}, data...)
	binary.LittleEndian.PutUint32(other[last+14:], 0x9999)
	got, err = audiofile.Duration(other)
	require.NoError(t, err)
	assert.Equal(t, time.Second, got)
}

func lastPageAt(data []byte) int {
	for i := len(data) - 4; i >= 0; i-- {
		if string(data[i:i+4]) == "OggS" {
			return i
		}
	}
	return -1
}

// A file with junk in front of its first MP3 frame (an ID3v2 tag that is padded, then zeros) and an ID3v1 tag after the last is the length of the frames.
func TestDurationOfAnMP3SkipsJunkBeforeTheFirstFrameAndStopsAtATrailingTag(t *testing.T) {
	data := append([]byte{}, MP3(true, 0)...)
	data = append(data, make([]byte, 37)...)
	data = append(data, MP3(false, 100)...)
	data = append(data, append([]byte("TAG"), make([]byte, 125)...)...)
	got, err := audiofile.Duration(data)
	require.NoError(t, err)
	assert.InDelta(t, float64(hundredFrames), float64(got), float64(time.Microsecond))
}

// A box that says it is bigger than the file, or is 0 bytes long, or has a 64 bit size that would overflow, ends the walk with an error.
func TestDurationOfAnM4AWithBoxSizesThatLie(t *testing.T) {
	ftyp := M4A(0, 1000, 1000, true)[:20]
	for name, tail := range map[string][]byte{
		"a box larger than the file":     {0xFF, 0xFF, 0xFF, 0xFF, 'm', 'o', 'o', 'v'},
		"a box of the size 4":            {0, 0, 0, 4, 'f', 'r', 'e', 'e'},
		"a 64 bit size that overflows":   append([]byte{0, 0, 0, 1, 'm', 'o', 'o', 'v'}, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xF0),
		"a 64 bit size under its header": append([]byte{0, 0, 0, 1, 'f', 'r', 'e', 'e'}, 0, 0, 0, 0, 0, 0, 0, 3),
		"a box to the end that is junk":  {0, 0, 0, 0, 'j', 'u', 'n', 'k'},
	} {
		_, err := audiofile.Duration(append(append([]byte{}, ftyp...), tail...))
		assert.True(t, errors.Is(err, audiofile.ErrUnreadable), "%s: %v", name, err)
	}
}
