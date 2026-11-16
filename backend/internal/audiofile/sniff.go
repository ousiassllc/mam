package audiofile

import "bytes"

// Sniff reads the kind of an audio file from its first bytes, never from a name or a type the client sent:
//   - "RIFF....WAVE" is WAV;
//   - "fLaC" is FLAC;
//   - "OggS" whose first packet is a Vorbis or an Opus header is OGG (an Ogg of Theora video, Speex or FLAC-in-Ogg is not taken);
//   - an "ftyp" box is M4A when the file has no video track and its brand is "M4A " or "M4B ", or is "mp42" or "isom" (the brands of MP4 videos too, and
//     of many audio recorders) and its moov box has a sound track: the handler types in moov/trak/mdia/hdlr decide (isAudioMP4);
//   - an ID3v2 tag, then an MPEG audio frame header (layer III, a valid bitrate and sample rate), is MP3 (a FLAC under an ID3v2 tag is FLAC).
//
// false for anything else (an image, a text, an AAC in ADTS, an MP2...).
func Sniff(data []byte) (mime string, ok bool) {
	switch {
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		return WAV, true
	case len(data) >= 4 && string(data[0:4]) == "fLaC":
		return FLAC, true
	case len(data) >= 4 && string(data[0:4]) == "OggS":
		if _, _, ok := oggFirstPacket(data); ok {
			return OGG, true
		}
		return "", false
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		if isAudioMP4(data) {
			return M4A, true
		}
		return "", false
	}
	if n, ok := id3Size(data); ok { // an ID3v2 tag: what follows it tells MP3 from FLAC; a tag that is all there is (a cut file) is taken for MP3
		if rest := data[min(n, len(data)):]; bytes.HasPrefix(rest, []byte("fLaC")) {
			return FLAC, true
		}
		return MP3, true
	}
	if _, ok := parseMP3Header(data); ok {
		return MP3, true
	}
	return "", false
}

// id3Size is the length of the ID3v2 tag at the start of data (its 10 byte header, the body and the footer when there is one); false when data does not
// start with a valid tag header: "ID3", a version 2.2 to 2.4, and a size in four 7 bit bytes.
func id3Size(data []byte) (int, bool) {
	if len(data) < 10 || string(data[0:3]) != "ID3" || data[3] < 2 || data[3] > 4 || data[4] == 0xFF {
		return 0, false
	}
	if data[6]|data[7]|data[8]|data[9] >= 0x80 {
		return 0, false
	}
	n := 10 + (int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9]))
	if data[5]&0x10 != 0 { // the footer flag: a copy of the header after the body
		n += 10
	}
	return n, true
}
