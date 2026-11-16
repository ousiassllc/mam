package audiofile

import (
	"bytes"
	"time"
)

// flacDuration reads the length from the STREAMINFO block, the first metadata block of every FLAC file: the sample rate (20 bits) and the number of
// samples (36 bits). A stream whose encoder did not know the end writes 0 samples; that has no length and is refused.
func flacDuration(data []byte) (time.Duration, error) {
	if n, ok := id3Size(data); ok { // a FLAC under an ID3v2 tag
		data = data[min(n, len(data)):]
	}
	// "fLaC", then the block header (1 byte: last flag and type, 3 bytes: length), then the 34 bytes of STREAMINFO (type 0).
	if !bytes.HasPrefix(data, []byte("fLaC")) || len(data) < 4+4+34 || data[4]&0x7F != 0 {
		return 0, unreadable("no STREAMINFO block")
	}
	s := data[8 : 8+34]
	sampleRate := uint64(s[10])<<12 | uint64(s[11])<<4 | uint64(s[12])>>4
	samples := uint64(s[13]&0x0F)<<32 | uint64(s[14])<<24 | uint64(s[15])<<16 | uint64(s[16])<<8 | uint64(s[17])
	return lengthOrErr(samples, sampleRate)
}
