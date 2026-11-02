package export

import (
	"fmt"
	"strings"
	"time"
)

// jst is the zone of created_at in prompts.csv and of the time in a file name (the zone of the people who read the sheet; a
// fixed offset, so that it does not depend on the time zone database of the machine).
var jst = time.FixedZone("JST", 9*60*60)

const (
	stampLayout = "20060102-150405"
	csvLayout   = "2006-01-02 15:04:05"
	// modelNameMax is how many characters of the model id go into a file name.
	modelNameMax = 40
	// idTail is how many characters of the result id (its random end: the start of a UUIDv7 is the time) go into a file name.
	idTail = 6
)

// namePart makes s safe as a part of a file name: only ASCII letters, digits, "." "_" "-" stay, every other character
// (a path separator, a colon, a space, a control character, any non-ASCII letter) becomes "-", runs of "-" are one, runs of "."
// are one, and "-" and "." are trimmed from both ends, so that no part is a path or a name like "..". An empty result is fallback.
func namePart(s string, limit int, fallback string) string {
	var b strings.Builder
	dash := false
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_'
		switch {
		case ok:
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= limit*2 {
			break
		}
	}
	out := b.String()
	for strings.Contains(out, "..") {
		out = strings.ReplaceAll(out, "..", ".")
	}
	out = strings.Trim(out, "-.")
	if len(out) > limit {
		out = strings.Trim(out[:limit], "-.")
	}
	if out == "" {
		return fallback
	}
	return out
}

// extension is the extension of the stored file (lower case, letters and digits only, at most 5), else the one of the media type.
func extension(stored, mime string) string {
	if i := strings.LastIndexByte(stored, '.'); i >= 0 {
		ext := strings.ToLower(stored[i+1:])
		if ext != "" && len(ext) <= 5 && strings.Trim(ext, "abcdefghijklmnopqrstuvwxyz0123456789") == "" {
			return ext
		}
	}
	switch strings.ToLower(strings.SplitN(mime, ";", 2)[0]) {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	}
	return "bin"
}

// fileName is the name of an item in photos/: the number (at least two digits, as many as the largest number needs), the time
// the result was made, the model and the end of the result id: "01_20261101-173805_chroma1-hd_bce291.png".
func fileName(number, total int, created time.Time, model, resultID, stored, mime string) string {
	width := max(2, len(fmt.Sprint(total)))
	tail := namePart(strings.ReplaceAll(resultID, "-", ""), 64, "result")
	if len(tail) > idTail {
		tail = tail[len(tail)-idTail:]
	}
	return fmt.Sprintf("%0*d_%s_%s_%s.%s", width, number, created.In(jst).Format(stampLayout), namePart(model, modelNameMax, "model"), tail, extension(stored, mime))
}

// archiveName is the name of the ZIP: the time of the newest result in it and how many there are; a part of a split export
// says which one it is.
func archiveName(newest time.Time, count, part, parts int) string {
	name := "generation-results_" + newest.In(jst).Format(stampLayout) + "_" + fmt.Sprint(count)
	if parts > 1 {
		name += fmt.Sprintf("_part%dof%d", part, parts)
	}
	return name + ".zip"
}
