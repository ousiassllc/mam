// Package selector builds the CSS selectors that the pages hand to HTMX (hx-target) from IDs that
// come from outside. A bundle import keeps the ID of every row it finds, so an ID may hold "." (a
// class), ":" (a pseudo-class), a space or "#"; concatenated into "#shot-" + id it would select
// another element or be an invalid selector, and htmx:targetError would swallow the result of
// the action. paths does the same for URLs: nothing else builds an ID selector from an ID by hand.
package selector

import (
	"strconv"
	"strings"
)

// ID is the selector of the element whose id is prefix + id: ID("shot-", "a.b") is `#shot-a\.b`.
// The whole value is escaped like the browser's CSS.escape, which static/src/chat.ts and
// static/src/components/lib/shot.ts use for the same elements.
func ID(prefix, id string) string {
	return "#" + escape(prefix+id)
}

// escape is CSS.escape (CSSOM, "serialize an identifier").
func escape(s string) string {
	var sb strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == 0:
			sb.WriteRune('\uFFFD')
		case r < 0x20 || r == 0x7F,
			isDigit(r) && (i == 0 || (i == 1 && runes[0] == '-')):
			sb.WriteByte('\\')
			sb.WriteString(strconv.FormatInt(int64(r), 16))
			sb.WriteByte(' ')
		case r == '-' && len(runes) == 1:
			sb.WriteString(`\-`)
		case r >= 0x80, r == '-', r == '_', isDigit(r), r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			sb.WriteRune(r)
		default:
			sb.WriteByte('\\')
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }
