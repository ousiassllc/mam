// Package audiofiletest builds tiny synthetic audio files of every kind package audiofile reads, with the header layout real encoders write (a LIST
// chunk before the WAV data, an ID3v2 tag before the MP3 frames, a Vorbis and an Opus page sequence, a moov box after the media data): so tests need no
// binary fixture in the repository. The sound itself is silence (zero bytes) where it has to take room, and the files are as short as their length allows.
package audiofiletest

import (
	"bytes"
	"encoding/binary"
	"math"
)

// WAV is a 16 bit mono PCM WAV of the given number of samples at the given rate, with a LIST chunk between the "fmt " and "data" chunks.
func WAV(sampleRate, samples int) []byte {
	le := binary.LittleEndian
	var b bytes.Buffer
	fmtChunk := make([]byte, 16)
	le.PutUint16(fmtChunk[0:], 1) // PCM
	le.PutUint16(fmtChunk[2:], 1) // mono
	le.PutUint32(fmtChunk[4:], u32(sampleRate))
	le.PutUint32(fmtChunk[8:], u32(sampleRate*2)) // byte rate
	le.PutUint16(fmtChunk[12:], 2)                // block align
	le.PutUint16(fmtChunk[14:], 16)               // bits per sample
	list := []byte("INFOISFT\x05\x00\x00\x00mam\x00\x00")
	b.WriteString("RIFF\x00\x00\x00\x00WAVE") // the RIFF size is patched below
	b.WriteString("fmt ")
	_ = binary.Write(&b, le, u32(len(fmtChunk)))
	b.Write(fmtChunk)
	b.WriteString("LIST")
	_ = binary.Write(&b, le, u32(len(list)))
	b.Write(list)
	if len(list)%2 == 1 { // chunks are padded to an even size
		b.WriteByte(0)
	}
	b.WriteString("data")
	_ = binary.Write(&b, le, u32(samples*2))
	b.Write(make([]byte, samples*2))
	out := b.Bytes()
	le.PutUint32(out[4:], u32(len(out)-8))
	return out
}

// MP3 is an MPEG-1 layer III file of CBR frames (128 kbit/s, 44.1 kHz, 1152 samples and 417 bytes each: 26.12 ms), after an ID3v2.4 tag of 110 bytes
// when withID3 is true.
func MP3(withID3 bool, frames int) []byte {
	var out []byte
	if withID3 {
		out = append([]byte("ID3\x04\x00\x00\x00\x00\x00\x64"), make([]byte, 100)...)
	}
	for range frames {
		out = append(out, append([]byte{0xFF, 0xFB, 0x90, 0x00}, make([]byte, 417-4)...)...)
	}
	return out
}

// MP3MPEG2 is an MPEG-2 layer III file (64 kbit/s, 22.05 kHz, 576 samples and 208 bytes per frame: 26.12 ms) of the given number of frames.
func MP3MPEG2(frames int) []byte {
	var out []byte
	for range frames {
		out = append(out, append([]byte{0xFF, 0xF3, 0x80, 0x00}, make([]byte, 208-4)...)...)
	}
	return out
}

// FLAC is a FLAC file that is only its STREAMINFO block (mono, 16 bit) saying it has the given number of samples at the given rate.
func FLAC(sampleRate int, samples uint64) []byte {
	info := make([]byte, 34)
	binary.BigEndian.PutUint16(info[0:], 4096) // the smallest and the largest block size
	binary.BigEndian.PutUint16(info[2:], 4096)
	// 20 bits of sample rate, 3 of channels - 1, 5 of bits per sample - 1, 36 of samples, as one 64 bit field starting at byte 10
	binary.BigEndian.PutUint64(info[10:], u64(sampleRate)<<44|0<<41|15<<36|samples&(1<<36-1))
	return append([]byte("fLaC\x80\x00\x00\x22"), info...)
}

// oggPage is one Ogg page of a single packet (up to 255 x 255 bytes) with the given flags (2: first page of the stream, 4: last), granule position and
// serial number. The checksum is left 0: the reader does not check it.
func oggPage(flags byte, granule int64, serial, seq uint32, packet []byte) []byte {
	lacing := bytes.Repeat([]byte{255}, len(packet)/255)
	lacing = append(lacing, u8(len(packet)%255))
	h := make([]byte, 27)
	copy(h, "OggS")
	h[5] = flags
	_, _ = binary.Encode(h[6:], binary.LittleEndian, granule) // two's complement: -1 is all ones
	binary.LittleEndian.PutUint32(h[14:], serial)
	binary.LittleEndian.PutUint32(h[18:], seq)
	h[26] = u8(len(lacing))
	return append(append(h, lacing...), packet...)
}

// oggStream is the page sequence of a stream: the identification page, the comment page, a page of audio that has no packet ending on it (granule -1),
// a page in the middle and the last page, with the granule positions the codec counts.
func oggStream(serial uint32, ident []byte, middle, last int64) []byte {
	var b bytes.Buffer
	b.Write(oggPage(2, 0, serial, 0, ident))
	b.Write(oggPage(0, 0, serial, 1, []byte("\x03vorbis comment and setup")))
	b.Write(oggPage(0, middle, serial, 2, make([]byte, 600)))
	b.Write(oggPage(0, -1, serial, 3, make([]byte, 300))) // a packet continues over this page
	b.Write(oggPage(4, last, serial, 4, make([]byte, 40)))
	return b.Bytes()
}

// OGGVorbis is an Ogg Vorbis file of the given number of samples at the given rate (the granule position of its last page).
func OGGVorbis(sampleRate int, samples int64) []byte {
	ident := make([]byte, 30)
	copy(ident, "\x01vorbis")
	ident[11] = 2 // channels, after the type, "vorbis" and the version (4 bytes)
	binary.LittleEndian.PutUint32(ident[12:], u32(sampleRate))
	ident[29] = 1 // the framing bit
	return oggStream(0x1234, ident, samples/2, samples)
}

// OGGOpus is an Ogg Opus file that plays the given number of samples (at 48 kHz) after a pre-skip of preSkip samples: the granule position of its last page
// is the two together.
func OGGOpus(preSkip int, samples int64) []byte {
	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8], head[9] = 1, 2 // version, channels
	binary.LittleEndian.PutUint16(head[10:], u16(preSkip))
	binary.LittleEndian.PutUint32(head[12:], 44100) // the rate of the source: not what the length is counted in
	return oggStream(0xBEEF, head, (samples+int64(preSkip))/2, samples+int64(preSkip))
}

// OGGTheora is the start of an Ogg Theora (video) file: an Ogg, but not audio.
func OGGTheora() []byte {
	return oggPage(2, 0, 7, 0, append([]byte("\x80theora"), make([]byte, 40)...))
}

// box is an ISO base media box of the given type and content.
func box(kind string, content ...[]byte) []byte {
	body := bytes.Join(content, nil)
	out := binary.BigEndian.AppendUint32(nil, u32(8+len(body)))
	return append(append(out, kind...), body...)
}

// M4A is an M4A file (ftyp brand "M4A ", one sound track) of the movie header mvhd of version 0 or 1 saying the given timescale and duration; the moov box
// comes first (a file made for streaming) when moovFirst is true and after the media data (mdat) otherwise.
func M4A(version int, timescale uint32, duration uint64, moovFirst bool) []byte {
	return m4a("M4A ", version, timescale, duration, moovFirst, "soun")
}

// MP4WithBrand is the layout of M4A (the moov box first, version 0) with another major brand ("isom", "mp42", "M4B "...) and one track for each of the given
// handler types ("soun", "vide"); with none, a single video track: an MP4 video.
func MP4WithBrand(brand string, timescale uint32, duration uint64, handlers ...string) []byte {
	if len(handlers) == 0 {
		handlers = []string{"vide"}
	}
	return m4a(brand, 0, timescale, duration, true, handlers...)
}

// MP4MoovLast is MP4WithBrand with the moov box after the media data (a file that was not made for streaming).
func MP4MoovLast(brand string, timescale uint32, duration uint64, handlers ...string) []byte {
	return m4a(brand, 0, timescale, duration, false, handlers...)
}

// MP4NoMoov is the start of an MP4 of the brand: an ftyp box, a free box and the media data, and no moov box (a file that was cut before its end).
func MP4NoMoov(brand string) []byte {
	return bytes.Join([][]byte{box("ftyp", []byte(brand), []byte{0, 0, 0, 0}, []byte(brand), []byte("mp42")), box("free", make([]byte, 8)), box("mdat", make([]byte, 64))}, nil)
}

// MP4NoTracks is an MP4 of the brand whose moov box has a movie header and no track at all.
func MP4NoTracks(brand string) []byte { return m4a(brand, 0, 1000, 5000, true) }

// trak is a track box of the handler type, with a tkhd before and an minf after the hdlr as real files have.
func trak(handler string) []byte {
	hdlr := append(append([]byte{0, 0, 0, 0, 0, 0, 0, 0}, handler...), make([]byte, 12)...)
	return box("trak", box("tkhd", make([]byte, 84)), box("mdia", box("mdhd", make([]byte, 24)), box("hdlr", hdlr, []byte("handler\x00")), box("minf", make([]byte, 16))))
}

func m4a(brand string, version int, timescale uint32, duration uint64, moovFirst bool, handlers ...string) []byte {
	mvhd := make([]byte, 100)
	if version == 1 {
		mvhd = make([]byte, 112)
		mvhd[0] = 1
		binary.BigEndian.PutUint32(mvhd[20:], timescale)
		binary.BigEndian.PutUint64(mvhd[24:], duration)
	} else {
		binary.BigEndian.PutUint32(mvhd[12:], timescale)
		binary.BigEndian.PutUint32(mvhd[16:], u32of(duration))
	}
	children := [][]byte{box("mvhd", mvhd)}
	for _, h := range handlers {
		children = append(children, trak(h))
	}
	children = append(children, box("udta", make([]byte, 12)))
	ftyp := box("ftyp", []byte(brand), []byte{0, 0, 0, 0}, []byte(brand), []byte("mp42"))
	moov := box("moov", children...)
	mdat := box("mdat", make([]byte, 64))
	if moovFirst {
		return bytes.Join([][]byte{ftyp, moov, mdat}, nil)
	}
	return bytes.Join([][]byte{ftyp, box("free", make([]byte, 8)), mdat, moov}, nil)
}

// The fixtures write sizes and rates into fields of a fixed width; a value that does not fit is a mistake of the test, and the fixture stops there.

func u8(n int) byte {
	if n < 0 || n > math.MaxUint8 {
		panic("audiofiletest: the value does not fit 8 bits")
	}
	return byte(n)
}

func u16(n int) uint16 {
	if n < 0 || n > math.MaxUint16 {
		panic("audiofiletest: the value does not fit 16 bits")
	}
	return uint16(n)
}

func u32(n int) uint32 {
	if n < 0 || n > math.MaxUint32 {
		panic("audiofiletest: the value does not fit 32 bits")
	}
	return uint32(n)
}

func u32of(n uint64) uint32 {
	if n > math.MaxUint32 {
		panic("audiofiletest: the value does not fit 32 bits")
	}
	return uint32(n)
}

func u64(n int) uint64 {
	if n < 0 {
		panic("audiofiletest: the value is negative")
	}
	return uint64(n)
}
