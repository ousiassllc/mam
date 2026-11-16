// Package uploadsniff checks the first bytes of an attachment against what its extension says (spec/requirements/non-functional.md §5 入力検証), so that a
// renamed executable or document is rejected even though its name looks harmless. Images are read by net/http's sniffer, videos by their container signature,
// audio files (Issue #1153) by audiofile.Sniff: the bytes must be the kind the extension names, and nothing is decoded. A video upload (.mp4 / .mov) is checked
// by its container signature only, never by its tracks, so an audio-only MP4 (an M4A) named .mp4 is taken as a video: it is stored as a video, never used as audio.
package uploadsniff

import (
	"bytes"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

const (
	ftypOffset      = 4
	ftypBrandOffset = 8
	ftypBrandEnd    = 12
	quickTimeBrand  = "qt  "
)

// Matches reports whether head (the first bytes of the file) is a file of the extension ext (lower case, with the dot), which the service stores as the asset
// type t with the media type mime.
func Matches(ext string, t domain.AssetType, mime string, head []byte) bool {
	switch t {
	case domain.AssetImage:
		return http.DetectContentType(head) == mime
	case domain.AssetAudio:
		sniffed, ok := audiofile.Sniff(head)
		return ok && sniffed == mime
	}
	switch ext {
	case ".webm":
		return bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}) // EBML header
	case ".mp4", ".mov":
		if len(head) < ftypBrandEnd || string(head[ftypOffset:ftypBrandOffset]) != "ftyp" {
			return false
		}
		quickTime := string(head[ftypBrandOffset:ftypBrandEnd]) == quickTimeBrand
		return ext == ".mov" || !quickTime
	}
	return false
}
