package audiofile

import "time"

// mp3Header is what the walk needs of an MPEG audio frame header.
type mp3Header struct {
	frameLen   int // bytes, the header included
	samples    int // samples per channel in the frame
	sampleRate int
}

// The bitrates (kbit/s) of layer III by the 4 bit index: MPEG-1, and MPEG-2 / 2.5. Index 0 is "free format" and 15 is invalid: both are refused.
var (
	mp3BitratesV1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	mp3BitratesV2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	// mp3SampleRates are by the version bits (3: MPEG-1, 2: MPEG-2, 0: MPEG-2.5) and the 2 bit index.
	mp3SampleRates = map[byte][3]int{3: {44100, 48000, 32000}, 2: {22050, 24000, 16000}, 0: {11025, 12000, 8000}}
)

// mp3SearchLimit is how far past an ID3v2 tag the first frame is looked for (a tag may be padded, or followed by junk).
const mp3SearchLimit = 64 << 10

// parseMP3Header reads the 4 byte header of a layer III frame at the start of b; false when b does not start with a valid one (no sync, the reserved
// version, another layer, a free or invalid bitrate, the reserved sample rate).
func parseMP3Header(b []byte) (mp3Header, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return mp3Header{}, false
	}
	version, layer := b[1]>>3&3, b[1]>>1&3
	rates, ok := mp3SampleRates[version]
	srIdx, brIdx, padding := b[2]>>2&3, b[2]>>4, int(b[2]>>1&1)
	if version == 1 || layer != 1 || !ok || srIdx == 3 || brIdx == 0 || brIdx == 15 {
		return mp3Header{}, false
	}
	h := mp3Header{sampleRate: rates[srIdx]}
	if version == 3 {
		h.samples, h.frameLen = 1152, 144*mp3BitratesV1[brIdx]*1000/h.sampleRate+padding
	} else {
		h.samples, h.frameLen = 576, 72*mp3BitratesV2[brIdx]*1000/h.sampleRate+padding
	}
	return h, true
}

// mp3Duration walks the frames after the ID3v2 tag, if any, and adds up their samples: a file of a variable bitrate has no length in its header that can be
// trusted (the Xing frame is optional and a lie as often as not), and a walk is cheap (at most one step per 24 bytes). It stops at the first byte that is
// no frame (an ID3v1 or APE tag, the end, a cut frame).
func mp3Duration(data []byte) (time.Duration, error) {
	start := 0
	if n, ok := id3Size(data); ok {
		start = min(n, len(data))
	}
	pos := mp3FirstFrame(data, start)
	if pos < 0 {
		return 0, unreadable("no MPEG audio frame")
	}
	first, _ := parseMP3Header(data[pos:])
	samples := 0
	for pos < len(data) {
		h, ok := parseMP3Header(data[pos:])
		if !ok || h.sampleRate != first.sampleRate || h.samples != first.samples {
			break
		}
		samples += h.samples
		pos += h.frameLen
	}
	return lengthOrErr(u64(samples), u64(first.sampleRate))
}

// mp3FirstFrame is the offset of the first frame at or after start: a valid header that the next frame follows (or that is the last the data holds), within
// mp3SearchLimit; -1 when there is none.
func mp3FirstFrame(data []byte, start int) int {
	for i := start; i < len(data)-3 && i <= start+mp3SearchLimit; i++ {
		h, ok := parseMP3Header(data[i:])
		if !ok {
			continue
		}
		if next := i + h.frameLen; next+4 > len(data) {
			return i
		} else if n, ok := parseMP3Header(data[next:]); ok && n.sampleRate == h.sampleRate {
			return i
		}
	}
	return -1
}
