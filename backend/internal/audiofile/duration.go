package audiofile

import "time"

// Duration is the length of the sound in an audio file, read from its container (Sniff decides the kind first):
//   - WAV: the "data" chunk over the byte rate of the "fmt " chunk;
//   - MP3: the samples of the frames, walked after the ID3v2 tag;
//   - FLAC: the sample count and rate of STREAMINFO;
//   - OGG: the granule position of the last page over the sample rate (Vorbis), or over 48 kHz less the pre-skip (Opus);
//   - M4A: the duration over the timescale of moov/mvhd.
//
// The error wraps ErrUnreadable: the bytes are no audio file of those kinds, the header is cut or corrupt, or the length is zero (a FLAC or WAV stream
// written without its length). It never panics, and it reads each byte at most a few times.
func Duration(data []byte) (time.Duration, error) {
	mime, ok := Sniff(data)
	if !ok {
		return 0, unreadable("not a WAV, MP3, FLAC, OGG or M4A file")
	}
	switch mime {
	case WAV:
		return wavDuration(data)
	case MP3:
		return mp3Duration(data)
	case FLAC:
		return flacDuration(data)
	case OGG:
		return oggDuration(data)
	default:
		return m4aDuration(data)
	}
}
