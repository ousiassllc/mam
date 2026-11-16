package audiofile

import (
	"encoding/binary"
	"math"
	"time"
)

// mp4Box is the header of an ISO base media box (an MP4 atom): where its content starts and ends in the data (the end is cut to the data that is there).
type mp4Box struct {
	kind       string
	start, end int
}

// nextMP4Box reads the box header at data[pos:end]; false when it is cut or says it is smaller than its header. A size of 0 means "to the end", 1 that a
// 64 bit size follows.
func nextMP4Box(data []byte, pos, end int) (mp4Box, bool) {
	avail := end - pos
	if avail < 8 {
		return mp4Box{}, false
	}
	size, hdr := uint64(binary.BigEndian.Uint32(data[pos:])), 8
	switch size {
	case 0:
		size = uint64(avail)
	case 1:
		if avail < 16 {
			return mp4Box{}, false
		}
		size, hdr = binary.BigEndian.Uint64(data[pos+8:]), 16
	}
	if size < uint64(hdr) {
		return mp4Box{}, false
	}
	boxEnd := end // a size that runs past the data is cut to it
	if size < uint64(avail) && size <= math.MaxInt {
		boxEnd = pos + int(size)
	}
	return mp4Box{kind: string(data[pos+4 : pos+8]), start: pos + hdr, end: boxEnd}, true
}

// findMP4Box walks the sibling boxes in data[pos:end] for the first of the kind; each step moves past a box of at least 8 bytes, so the walk ends.
func findMP4Box(data []byte, pos, end int, kind string) (mp4Box, bool) {
	for {
		b, ok := nextMP4Box(data, pos, end)
		if !ok {
			return mp4Box{}, false
		}
		if b.kind == kind {
			return b, true
		}
		pos = b.end
	}
}

// m4aDuration is the duration of the movie header (moov/mvhd) over its timescale. The moov box may come after the media data (a file that was not made for
// streaming): the walk skips the boxes by their sizes, so it never reads the media. The length is the length of the whole movie; the brand check of
// Sniff is what keeps an MP4 video out.
func m4aDuration(data []byte) (time.Duration, error) {
	moov, ok := findMP4Box(data, 0, len(data), "moov")
	if !ok {
		return 0, unreadable("no moov box")
	}
	mvhd, ok := findMP4Box(data, moov.start, moov.end, "mvhd")
	if !ok {
		return 0, unreadable("no mvhd box")
	}
	b := data[mvhd.start:mvhd.end]
	var timescale, duration uint64
	switch {
	case len(b) >= 20 && b[0] == 0: // version 0: 32 bit creation and modification times, timescale, duration
		timescale, duration = uint64(binary.BigEndian.Uint32(b[12:])), uint64(binary.BigEndian.Uint32(b[16:]))
		if duration == 0xFFFFFFFF { // "unknown"
			duration = 0
		}
	case len(b) >= 32 && b[0] == 1: // version 1: 64 bit times and duration
		timescale, duration = uint64(binary.BigEndian.Uint32(b[20:])), binary.BigEndian.Uint64(b[24:])
		if duration == 1<<64-1 {
			duration = 0
		}
	default:
		return 0, unreadable("the mvhd box is cut or of an unknown version")
	}
	return lengthOrErr(duration, timescale)
}

// mp4Handlers walks moov/trak/mdia/hdlr and says whether the movie has a sound track (handler type "soun") and a video track ("vide"); found is false when
// the moov box cannot be found or has no track. It follows boxes only, never bytes, and each step moves past a box of at least 8 bytes, so the work is
// bounded by the number of boxes in the data. A box the data cuts short is read as far as it goes: a track whose hdlr is cut says nothing.
func mp4Handlers(data []byte) (sound, video, found bool) {
	moov, ok := findMP4Box(data, 0, len(data), "moov")
	if !ok {
		return false, false, false
	}
	for pos := moov.start; ; {
		b, ok := nextMP4Box(data, pos, moov.end)
		if !ok {
			return sound, video, found
		}
		pos = b.end
		if b.kind != "trak" {
			continue
		}
		mdia, ok := findMP4Box(data, b.start, b.end, "mdia")
		if !ok {
			continue
		}
		hdlr, ok := findMP4Box(data, mdia.start, mdia.end, "hdlr")
		if !ok || hdlr.end-hdlr.start < 12 { // version and flags, pre_defined, then the 4 bytes of the handler type
			continue
		}
		found = true
		switch string(data[hdlr.start+8 : hdlr.start+12]) {
		case "soun":
			sound = true
		case "vide":
			video = true
		}
	}
}

// isAudioMP4 tells whether the file whose ftyp box is at its start is an audio file. The major brand "M4A " or "M4B " says audio: such a file is refused
// only when its tracks include a video track. The brands "mp42" and "isom" are the ones of MP4 videos too, and are many an Android recorder's or ffmpeg's
// brand for an audio file: such a file is taken only when its moov box is found and has a sound track and no video track. Any other brand is not taken.
func isAudioMP4(data []byte) bool {
	sound, video, _ := mp4Handlers(data)
	switch string(data[8:12]) {
	case "M4A ", "M4B ":
		return !video
	case "mp42", "isom":
		return sound && !video
	}
	return false
}
