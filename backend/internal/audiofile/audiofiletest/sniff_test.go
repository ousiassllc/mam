package audiofiletest

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
)

var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestSniffReadsTheKindFromTheBytes(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		mime string
	}{
		{"wav", WAV(8000, 80), audiofile.WAV},
		{"mp3 with an ID3 tag", MP3(true, 2), audiofile.MP3},
		{"mp3 with an ID3 tag that is all there is", []byte("ID3\x04\x00\x00\x00\x00\x00\x00"), audiofile.MP3},
		{"mp3 with a bare frame header", MP3(false, 2), audiofile.MP3},
		{"mp3 of MPEG-2", MP3MPEG2(2), audiofile.MP3},
		{"flac", FLAC(44100, 44100), audiofile.FLAC},
		{"flac under an ID3 tag", append(MP3(true, 0), FLAC(44100, 44100)...), audiofile.FLAC},
		{"ogg vorbis", OGGVorbis(44100, 44100), audiofile.OGG},
		{"ogg opus", OGGOpus(312, 48000), audiofile.OGG},
		{"m4a", M4A(0, 44100, 44100, true), audiofile.M4A},
		{"m4b (an audiobook)", MP4WithBrand("M4B ", 44100, 44100, "soun"), audiofile.M4A},
		{"m4a brand with a sound track and no more (a recorder's file)", MP4WithBrand("M4A ", 44100, 44100, "soun"), audiofile.M4A},
		{"m4a brand whose moov is not there (cut file: not known to have a video)", MP4NoMoov("M4A "), audiofile.M4A},
		{"isom with a sound track only", MP4WithBrand("isom", 44100, 44100, "soun"), audiofile.M4A},
		{"isom with a sound track only, the moov box after the media data", MP4MoovLast("isom", 44100, 44100, "soun"), audiofile.M4A},
		{"mp42 with a video track, the moov box after the media data", MP4MoovLast("mp42", 44100, 44100, "vide"), ""},
		{"mp42 with a sound track only", MP4WithBrand("mp42", 44100, 44100, "soun"), audiofile.M4A},
		{"mp42 with two sound tracks", MP4WithBrand("mp42", 44100, 44100, "soun", "soun"), audiofile.M4A},
		// what is not audio, or not one of the kinds
		{"mp3 at 2 bytes short of a header", MP3(false, 1)[:2], ""},
		{"png", png, ""},
		{"text", []byte("hello, this is not a sound"), ""},
		{"a flac that is cut inside its marker", []byte("fLa"), ""},
		{"riff that is not wave (an AVI)", []byte("RIFF\x00\x00\x00\x00AVI LIST"), ""},
		{"aac in an ADTS frame (layer bits 00)", []byte{0xFF, 0xF1, 0x50, 0x80, 0x00}, ""},
		{"mpeg audio layer II", []byte{0xFF, 0xFD, 0x90, 0x00}, ""},
		{"mpeg audio with the reserved version", []byte{0xFF, 0xEB, 0x90, 0x00}, ""},
		{"mpeg audio with the free bitrate", []byte{0xFF, 0xFB, 0x00, 0x00}, ""},
		{"mpeg audio with the bad bitrate", []byte{0xFF, 0xFB, 0xF0, 0x00}, ""},
		{"mpeg audio with the reserved sample rate", []byte{0xFF, 0xFB, 0x9C, 0x00}, ""},
		{"ogg theora (a video)", OGGTheora(), ""},
		{"ogg that is only its marker", []byte("OggS"), ""},
		{"ogg page that does not begin the stream", append([]byte{}, patchFlags(OGGVorbis(44100, 100), 0)...), ""},
		{"mp4 video of the brand isom (video track only)", MP4WithBrand("isom", 1000, 5000, "vide"), ""},
		{"mp4 video of the brand mp42 with sound and video tracks", MP4WithBrand("mp42", 1000, 5000, "soun", "vide"), ""},
		{"mp4 video of the brand mp42 with video and sound tracks", MP4WithBrand("mp42", 1000, 5000, "vide", "soun"), ""},
		{"m4a brand with a video track", MP4WithBrand("M4A ", 1000, 5000, "soun", "vide"), ""},
		{"m4b brand with a video track only", MP4WithBrand("M4B ", 1000, 5000, "vide"), ""},
		{"isom without a moov box", MP4NoMoov("isom"), ""},
		{"mp42 without a moov box", MP4NoMoov("mp42"), ""},
		{"isom with a moov box that has no track", MP4NoTracks("isom"), ""},
		{"isom with a track of another kind (subtitles)", MP4WithBrand("isom", 1000, 5000, "text"), ""},
		{"mp4 of the brand avc1 with a sound track", MP4WithBrand("avc1", 1000, 5000, "soun"), ""},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mime, ok := audiofile.Sniff(c.data)
			assert.Equal(t, c.mime != "", ok)
			assert.Equal(t, c.mime, mime)
		})
	}
}

// patchFlags clears the header-type flags of the first page: a stream whose first page is not marked "beginning of stream".
func patchFlags(data []byte, flags byte) []byte {
	out := append([]byte{}, data...)
	out[5] = flags
	return out
}

// An ID3 prefix alone is not an MP3: a text that happens to start with "ID3" has no valid tag header (version, flags, size bytes of 7 bits).
func TestSniffRefusesAnID3PrefixThatIsNoTagHeader(t *testing.T) {
	for name, data := range map[string][]byte{
		"text starting with ID3":   []byte("ID3 is the name of a tag"),
		"size bytes over 7 bits":   []byte("ID3\x04\x00\x00\xff\xff\xff\xff"),
		"a version that is no tag": []byte("ID3\x09\x00\x00\x00\x00\x00\x00"),
		"a version 0":              []byte("ID3\x00\x00\x00\x00\x00\x00\x00"),
	} {
		_, ok := audiofile.Sniff(data)
		assert.False(t, ok, name)
	}
}
