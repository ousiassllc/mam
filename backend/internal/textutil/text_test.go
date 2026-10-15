package textutil_test

import (
	"testing"

	"github.com/ousiassllc/mam/backend/internal/textutil"
)

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"shorter", "abc", 5, "abc"},
		{"exact", "abcde", 5, "abcde"},
		{"cut ascii", "abcdef", 5, "abcde"},
		{"cut multibyte by rune", "あいうえお", 3, "あいう"},
		{"multibyte exact", "あいう", 3, "あいう"},
		{"zero", "abc", 0, ""},
		{"negative", "abc", -1, ""},
		{"empty", "", 3, ""},
		{"keeps whitespace", "  ab  ", 4, "  ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := textutil.TruncateRunes(tt.in, tt.max); got != tt.want {
				t.Errorf("TruncateRunes(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

func TestTruncateRunesEllipsis(t *testing.T) {
	if got := textutil.TruncateRunesEllipsis("あいうえお", 3); got != "あいう…" {
		t.Errorf("cut = %q", got)
	}
	if got := textutil.TruncateRunesEllipsis("あいう", 3); got != "あいう" {
		t.Errorf("exact = %q", got)
	}
}

func TestShortID(t *testing.T) {
	if got := textutil.ShortID("0190a1b2-c3d4-7000-8000-000000000000"); got != "0190a1b2" {
		t.Errorf("long = %q", got)
	}
	if got := textutil.ShortID("abc"); got != "abc" {
		t.Errorf("short = %q", got)
	}
	if got := textutil.ShortID("12345678"); got != "12345678" {
		t.Errorf("exact = %q", got)
	}
}
