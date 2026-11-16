package audiofile

import (
	"bytes"
	"encoding/binary"
	"time"
)

// oggPage is what the reader needs of the header of an Ogg page (RFC 3533): 27 bytes, then one byte of lacing value per segment, then the segments.
type oggPage struct {
	flags    byte
	granule  int64 // the position of the end of the last packet that ends on the page, in samples; -1 when none ends on it
	serial   uint32
	bodyAt   int // offset of the first byte of the body
	bodySize int
}

// oggTailWindow is how far from the end of the file the last page is looked for (a page is at most 65307 bytes).
const oggTailWindow = 128 << 10

// parseOggPage reads the page header at data[pos:]; false when it is not a valid one or the header is cut.
func parseOggPage(data []byte, pos int) (oggPage, bool) {
	h := data[pos:]
	if len(h) < 27 || string(h[0:4]) != "OggS" || h[4] != 0 || len(h) < 27+int(h[26]) {
		return oggPage{}, false
	}
	p := oggPage{flags: h[5], granule: granuleOf(h[6:]), serial: binary.LittleEndian.Uint32(h[14:]), bodyAt: pos + 27 + int(h[26])}
	for _, lace := range h[27 : 27+int(h[26])] {
		p.bodySize += int(lace)
	}
	return p, true
}

// granuleOf reads a granule position: 8 bytes, little endian, signed (all ones is -1, "no packet ends on the page").
func granuleOf(b []byte) int64 {
	var g int64
	_, _ = binary.Decode(b, binary.LittleEndian, &g) // b is at least 8 bytes: the caller checked the header length
	return g
}

// The codecs of an Ogg audio stream that are taken, with what the first packet says that the length needs: Vorbis the sample rate, Opus the pre-skip.
type oggCodec int

const (
	oggVorbis oggCodec = iota + 1
	oggOpus
)

// oggFirstPacket reads the identification packet of the stream that starts the file: the first packet of the first page (a beginning-of-stream page). ok is
// false for a file that is not an Ogg, or whose stream is not Vorbis or Opus (Theora, Speex, FLAC in Ogg...).
func oggFirstPacket(data []byte) (codec oggCodec, param uint32, ok bool) {
	page, ok := parseOggPage(data, 0)
	if !ok || page.flags&0x02 == 0 {
		return 0, 0, false
	}
	packet := data[page.bodyAt:]
	switch {
	case bytes.HasPrefix(packet, []byte("\x01vorbis")) && len(packet) >= 16 && binary.LittleEndian.Uint32(packet[7:]) == 0:
		return oggVorbis, binary.LittleEndian.Uint32(packet[12:]), true
	case bytes.HasPrefix(packet, []byte("OpusHead")) && len(packet) >= 19 && packet[8]>>4 == 0:
		return oggOpus, uint32(binary.LittleEndian.Uint16(packet[10:])), true
	}
	return 0, 0, false
}

// oggDuration is the granule position of the last complete page of the first stream, over the sample rate (Vorbis) or 48 kHz less the pre-skip (Opus:
// its granule positions are always at 48 kHz and count the pre-skip samples the decoder drops).
func oggDuration(data []byte) (time.Duration, error) {
	codec, param, ok := oggFirstPacket(data)
	if !ok {
		return 0, unreadable("not an Ogg Vorbis or Opus stream")
	}
	first, _ := parseOggPage(data, 0)
	for end := len(data); ; {
		i := bytes.LastIndex(data[:end], []byte("OggS"))
		if i < 0 || i < len(data)-oggTailWindow {
			return 0, unreadable("no complete Ogg page with a position")
		}
		end = i // the next search looks before this page: it always moves back
		p, ok := parseOggPage(data, i)
		if !ok || p.serial != first.serial || p.granule < 0 || p.bodyAt+p.bodySize > len(data) {
			continue
		}
		if codec == oggVorbis {
			return lengthOrErr(uint64(p.granule), uint64(param))
		}
		if p.granule <= int64(param) {
			return 0, unreadable("the stream ends inside its pre-skip")
		}
		return lengthOrErr(uint64(p.granule)-uint64(param), 48000)
	}
}
