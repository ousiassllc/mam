// Package textutil holds small string helpers shared by the agents and the services (cutting a text
// to a number of runes, shortening an ID).
package textutil

import "unicode/utf8"

// shortIDLen is the number of leading characters of an ID shown to the user.
const shortIDLen = 8

// TruncateRunes cuts s to at most max runes. It does not trim whitespace: a caller that wants that
// applies strings.TrimSpace first.
func TruncateRunes(s string, max int) string {
	if max < 0 {
		max = 0
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	n := 0
	for i := range s {
		if n == max {
			return s[:i]
		}
		n++
	}
	return s
}

// TruncateRunesEllipsis is TruncateRunes that appends "…" (after the max runes) when it cut s.
func TruncateRunesEllipsis(s string, max int) string {
	if t := TruncateRunes(s, max); len(t) != len(s) {
		return t + "…"
	}
	return s
}

// ShortID returns the first 8 characters of an ID, the form shown in chat notices.
func ShortID(id string) string {
	if len(id) > shortIDLen {
		return id[:shortIDLen]
	}
	return id
}
